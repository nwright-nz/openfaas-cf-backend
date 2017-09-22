// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	"code.cloudfoundry.org/garden/client"
	"code.cloudfoundry.org/garden/client/connection"
	"github.com/Sirupsen/logrus"
	natsHandler "github.com/alexellis/faas-nats/handler"
	internalHandlers "github.com/nwright-nz/openfaas-guardian-backend/handlers"
	"github.com/nwright-nz/openfaas-guardian-backend/metrics"
	"github.com/nwright-nz/openfaas-guardian-backend/plugin"
	"github.com/nwright-nz/openfaas-guardian-backend/types"

	"github.com/gorilla/mux"
)

type handlerSet struct {
	Proxy          http.HandlerFunc
	DeployFunction http.HandlerFunc
	DeleteFunction http.HandlerFunc
	ListFunctions  http.HandlerFunc
	Alert          http.HandlerFunc
	RoutelessProxy http.HandlerFunc

	// QueuedProxy - queue work and return synchronous response
	QueuedProxy http.HandlerFunc

	// AsyncReport - report a defered execution result
	AsyncReport http.HandlerFunc
}

func main() {

	logger := logrus.Logger{}

	logrus.SetFormatter(&logrus.TextFormatter{})

	osEnv := types.OsEnv{}
	readConfig := types.ReadConfig{}
	config := readConfig.Read(osEnv)

	log.Printf("HTTP Read Timeout: %s", config.ReadTimeout)
	log.Printf("HTTP Write Timeout: %s", config.WriteTimeout)
	//cfHost, gardenPort := config.GuardianHost, config.GuardianPort

	cfAddress := "https://" + config.CFUrl

	//gardenClient := client.New(connection.New("tcp", gardenAddress))
	gardenClient := client.New(connection.New("tcp", "10.10.149.106:7777"))
	capacity, err := gardenClient.Capacity()
	if err != nil {
		log.Fatal("Error retrieving guardian stats")
	} else {
		log.Printf("Successful connection. Guardian current capacity: %d Memory in Bytes, %d Disk, %d Maximum number of containers",
			capacity.MemoryInBytes, capacity.DiskInBytes, capacity.MaxContainers)
	}

	metricsOptions := metrics.BuildMetricsOptions()
	metrics.RegisterMetrics(metricsOptions)

	var faasHandlers handlerSet

	if config.UseExternalProvider() {

		reverseProxy := httputil.NewSingleHostReverseProxy(config.FunctionsProviderURL)

		faasHandlers.Proxy = internalHandlers.MakeForwardingProxyHandler(reverseProxy, &metricsOptions)
		faasHandlers.RoutelessProxy = internalHandlers.MakeForwardingProxyHandler(reverseProxy, &metricsOptions)
		faasHandlers.ListFunctions = internalHandlers.MakeForwardingProxyHandler(reverseProxy, &metricsOptions)
		faasHandlers.DeployFunction = internalHandlers.MakeForwardingProxyHandler(reverseProxy, &metricsOptions)
		faasHandlers.DeleteFunction = internalHandlers.MakeForwardingProxyHandler(reverseProxy, &metricsOptions)
		alertHandler := plugin.NewExternalServiceQuery(*config.FunctionsProviderURL)
		faasHandlers.Alert = internalHandlers.MakeAlertHandler(alertHandler)

		metrics.AttachExternalWatcher(*config.FunctionsProviderURL, metricsOptions, "func", time.Second*5)

	} else {
		maxRestarts := uint64(5)
		print(maxRestarts)
		faasHandlers.Proxy = internalHandlers.MakeProxy(metricsOptions, true, gardenClient, &logger)
		faasHandlers.RoutelessProxy = internalHandlers.MakeProxy(metricsOptions, true, gardenClient, &logger)
		faasHandlers.ListFunctions = internalHandlers.MakeFunctionReader(metricsOptions, gardenClient)
		faasHandlers.DeployFunction = internalHandlers.MakeNewFunctionHandler(metricsOptions, gardenClient, maxRestarts)
		faasHandlers.DeleteFunction = internalHandlers.MakeDeleteFunctionHandler(metricsOptions, gardenClient)

		//Nigel - To implement the alerting/scaling.
		//faasHandlers.Alert = internalHandlers.MakeAlertHandler(internalHandlers.NewSwarmServiceQuery(gardenClient))

		// This could exist in a separate process - records the replicas of each swarm service.
		//functionLabel := "function"
		//metrics.AttachSwarmWatcher(dockerClient, metricsOptions, functionLabel)
	}

	if config.UseNATS() {
		log.Println("Async enabled: Using NATS Streaming.")
		natsQueue, queueErr := natsHandler.CreateNatsQueue(*config.NATSAddress, *config.NATSPort)
		if queueErr != nil {
			log.Fatalln(queueErr)
		}

		faasHandlers.QueuedProxy = internalHandlers.MakeQueuedProxy(metricsOptions, true, &logger, natsQueue)
		faasHandlers.AsyncReport = internalHandlers.MakeAsyncReport(metricsOptions)
	}

	listFunctions := metrics.AddMetricsHandler(faasHandlers.ListFunctions, config.PrometheusHost, config.PrometheusPort)

	r := mux.NewRouter()

	// r.StrictSlash(false)	// This didn't work, so register routes twice.
	r.HandleFunc("/function/{name:[-a-zA-Z_0-9]+}", faasHandlers.Proxy)
	r.HandleFunc("/function/{name:[-a-zA-Z_0-9]+}/", faasHandlers.Proxy)

	// TODO: implement alerting
	//r.HandleFunc("/system/alert", faasHandlers.Alert)
	r.HandleFunc("/system/functions", listFunctions).Methods("GET")
	r.HandleFunc("/system/functions", faasHandlers.DeployFunction).Methods("POST")
	r.HandleFunc("/system/functions", faasHandlers.DeleteFunction).Methods("DELETE")

	if faasHandlers.QueuedProxy != nil {
		r.HandleFunc("/async-function/{name:[-a-zA-Z_0-9]+}/", faasHandlers.QueuedProxy).Methods("POST")
		r.HandleFunc("/async-function/{name:[-a-zA-Z_0-9]+}", faasHandlers.QueuedProxy).Methods("POST")
		//TODO: implement alerting
		//	r.HandleFunc("/system/async-report", faasHandlers.AsyncReport)
	}

	fs := http.FileServer(http.Dir("./assets/"))
	r.PathPrefix("/ui/").Handler(http.StripPrefix("/ui", fs)).Methods("GET")

	r.HandleFunc("/", faasHandlers.RoutelessProxy).Methods("POST")

	metricsHandler := metrics.PrometheusHandler()
	r.Handle("/metrics", metricsHandler)
	r.Handle("/", http.RedirectHandler("/ui/", http.StatusMovedPermanently)).Methods("GET")

	//tcpPort := 8080
	tcpPort, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		log.Fatal(err)
	}
	s := &http.Server{
		Addr:           fmt.Sprintf(":%d", tcpPort),
		ReadTimeout:    config.ReadTimeout,
		WriteTimeout:   config.WriteTimeout,
		MaxHeaderBytes: http.DefaultMaxHeaderBytes, // 1MB - can be overridden by setting Server.MaxHeaderBytes.
		Handler:        r,
	}

	log.Fatal(s.ListenAndServe())
}