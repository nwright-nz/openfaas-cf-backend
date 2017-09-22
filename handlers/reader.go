// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/nwright-nz/openfaas-cf-backend/metrics"
	"github.com/nwright-nz/openfaas-cf-backend/requests"
)

// MakeFunctionReader gives a summary of Function structs with Docker service stats overlaid with Prometheus counters.
func MakeFunctionReader(metricsOptions metrics.MetricOptions, c *cfclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		services, err := c.ListApps()

		if err != nil {
			fmt.Println(err)
		}

		// TODO: Filter only "faas" functions (via metadata?)
		var functions []requests.Function

		for _, service := range services {
			functionProp, _ := service.Environment["function"]
			//envProcess, _ := service.Environment["fprocess"]

			if functionProp == "true" {
				containerName := service.Name
				imageName := service.DockerImage

				f := requests.Function{
					Name:            containerName,
					Image:           imageName,
					InvocationCount: 0,
					Replicas:        uint64(service.Instances),
					//EnvProcess:      envProcess.(string),
				}

				functions = append(functions, f)

				if err != nil {
					log.Println("There was an error retrieving info about the service: ", service)
				}
			}
		}

		functionBytes, _ := json.Marshal(functions)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(functionBytes)
	}
}
