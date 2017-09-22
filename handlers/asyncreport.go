// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/nwright-nz/openfaas-guardian-backend/metrics"
	"github.com/nwright-nz/openfaas-guardian-backend/requests"
)

// MakeAsyncReport makes a handler for asynchronous invocations to report back into.
func MakeAsyncReport(metrics metrics.MetricOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		report := requests.AsyncReport{}
		bytesOut, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(bytesOut, &report)

		//trackInvocation(report.FunctionName, metrics, report.StatusCode)
	}
}