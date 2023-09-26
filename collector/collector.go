// Copyright 2023 Billy Wooten
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collector

import (
	"fmt"
	"net/http"
	"openweather_exporter/geo"
	"strings"
	"time"

	"github.com/jellydator/ttlcache/v2"

	"github.com/codingsince1985/geo-golang/openstreetmap"
	log "github.com/sirupsen/logrus"

	owm "github.com/briandowns/openweathermap"
	"github.com/prometheus/client_golang/prometheus"
)

// OpenweatherCollector Define a struct for your collector that contains pointers
// to prometheus descriptors for each metric you wish to expose.
// Note you can also include fields of other types if they provide utility,
// but we just won't be exposing them as metrics.
var notFound = ttlcache.ErrNotFound

type OpenweatherCollector struct {
	ApiKey            string
	Cache             *ttlcache.Cache
	DegreesUnit       string
	Language          string
	Locations         []Location
	temperatureMetric *prometheus.Desc
	humidity          *prometheus.Desc
	feelslike         *prometheus.Desc
}

type Location struct {
	Location      string
	Latitude      float64
	Longitude     float64
	CacheKeyOWM   string
	CacheKeyPOWM  string
	CacheKeyUVOWM string
}

func resolveLocations(locations string) []Location {
	var res []Location

	for _, location := range strings.Split(locations, "|") {
		// Get Coords.
		latitude, longitude, err := geo.GetCoords(openstreetmap.Geocoder(), location)
		if err != nil {
			log.Fatal("failed to resolve location:", err)
		}
		cacheKeyOWM := fmt.Sprintf("OWM %s", location)
		cacheKeyPOWM := fmt.Sprintf("POWM %s", location)
		cacheKeyUVOWM := fmt.Sprintf("UVOWM %s", location)
		res = append(res, Location{Location: location, Latitude: latitude, Longitude: longitude, CacheKeyOWM: cacheKeyOWM, CacheKeyPOWM: cacheKeyPOWM, CacheKeyUVOWM: cacheKeyUVOWM})
	}
	return res
}

// NewOpenweatherCollector You must create a constructor for your collector that
// initializes every descriptor and returns a pointer to the collector
func NewOpenweatherCollector(degreesUnit string, language string, apikey string, locations string, cache *ttlcache.Cache) *OpenweatherCollector {

	return &OpenweatherCollector{
		ApiKey:      apikey,
		DegreesUnit: degreesUnit,
		Language:    language,
		Locations:   resolveLocations(locations),
		Cache:       cache,
		temperatureMetric: prometheus.NewDesc("openweather_temperature",
			"Current temperature in degrees",
			[]string{"location"}, nil,
		),
		humidity: prometheus.NewDesc("openweather_humidity",
			"Current relative humidity",
			[]string{"location"}, nil,
		),
		feelslike: prometheus.NewDesc("openweather_feelslike",
			"Current feels_like temperature in degrees",
			[]string{"location"}, nil,
		),
	}
}

// Describe Each and every collector must implement the Describe function.
// It essentially writes all descriptors to the prometheus desc channel.
func (collector *OpenweatherCollector) Describe(ch chan<- *prometheus.Desc) {

	//Update this section with the metric you create for a given collector
	ch <- collector.temperatureMetric
	ch <- collector.humidity
	ch <- collector.feelslike

}

// Collect implements required collect function for all prometheus collectors
func (collector *OpenweatherCollector) Collect(ch chan<- prometheus.Metric) {
	for _, location := range collector.Locations {
		var w *owm.CurrentWeatherData

		// Setup HTTP Client
		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		if val, err := collector.Cache.Get(location.CacheKeyOWM); err != notFound || val != nil {
			// Grab Metrics from cache
			w = val.(*owm.CurrentWeatherData)
		} else {
			// Grab Metrics
			w, err = owm.NewCurrent(collector.DegreesUnit, collector.Language, collector.ApiKey, owm.WithHttpClient(client))
			if err != nil {
				log.Fatal("invalid openweather API configuration:", err)
			}
			err = w.CurrentByCoordinates(&owm.Coordinates{Latitude: location.Latitude, Longitude: location.Longitude})
			if err != nil {
				log.Infof("Collecting metrics failed for %s: %s", location.Location, err.Error())
				continue
			}
			err = collector.Cache.Set(location.CacheKeyOWM, w)
			if err != nil {
				log.Infof("Could not set cache data. %s", err.Error())
				continue
			}
		}

		// Write the latest value for each metric in the prometheus metric channel.
		// Note that you can pass CounterValue, GaugeValue, or UntypedValue types here.
		ch <- prometheus.MustNewConstMetric(collector.temperatureMetric, prometheus.GaugeValue, w.Main.Temp, location.Location)
		ch <- prometheus.MustNewConstMetric(collector.humidity, prometheus.GaugeValue, float64(w.Main.Humidity), location.Location)
		ch <- prometheus.MustNewConstMetric(collector.feelslike, prometheus.GaugeValue, w.Main.FeelsLike, location.Location)
	}
}
