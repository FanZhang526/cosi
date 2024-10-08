/*
 Copyright (c) Huawei Technologies Co., Ltd. 2024-2024. All rights reserved.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

// Package main runs the main function
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	cosispec "sigs.k8s.io/container-object-storage-interface-spec"

	"github.com/huawei/cosi-driver/pkg/utils/log"
	"github.com/huawei/cosi-driver/pkg/utils/version"
)

var (
	probeTimeout  = flag.Duration("probe-timeout", time.Second, "Probe timeout in seconds.")
	driverAddress = flag.String("driver-address", "/var/lib/cosi/cosi.sock",
		"driver address for socket")
	httpEndpoint   = flag.String("http-endpoint", "", "HTTP server endPoint for health probe check")
	logFile        = flag.String("log-file", "liveness-probe", "The log file name of the liveness probe.")
	kubeConfigPath = flag.String("kube-config-path", "", "absolute path to the kubeConfig file")
)

const (
	containerName = "liveness-probe"
)

func main() {
	flag.Parse()

	err := log.InitLogging(*logFile)
	if err != nil {
		logrus.Errorf("init log failed, error is [%v]", err)
		return
	}

	ctx, err := log.SetRequestInfo(context.Background())
	if err != nil {
		log.Errorf("set request info failed, error is [%v]", err)
		return
	}

	err = version.RegisterVersion(containerName, version.LivenessProbeVersion, *kubeConfigPath)
	if err != nil {
		log.AddContext(ctx).Errorf("init version file failed, error is [%v]", err)
		return
	}

	signalChan := make(chan os.Signal, 1)
	defer close(signalChan)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGILL, syscall.SIGKILL, syscall.SIGTERM)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", checkProbe)
	go func(ch chan os.Signal) {
		err = http.ListenAndServe(*httpEndpoint, mux)
		if err != nil {
			log.AddContext(ctx).Errorf("probe http server listen [%s] failed, error is [%v]", httpEndpoint, err)
			ch <- syscall.SIGINT
			return
		}
	}(signalChan)
	log.AddContext(ctx).Infoln("start probe http server successfully!")

	stopSignal := <-signalChan
	log.AddContext(ctx).Warningf("stop probe service successfully, stopSignal is [%v]", stopSignal)
}

func checkProbe(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), *probeTimeout)
	defer cancel()

	unixPrefix := "unix://"
	providerAddress := unixPrefix + *driverAddress
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithIdleTimeout(time.Duration(0)),
	}
	conn, err := grpc.Dial(providerAddress, dialOptions...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.AddContext(ctx).Errorf("failed to establish connection to cosi provider, error is [%v]", err)
		return
	}
	defer conn.Close()

	log.AddContext(ctx).Infoln("sending probe request to cosi driver")
	client := cosispec.NewIdentityClient(conn)
	_, err = client.DriverGetInfo(ctx, &cosispec.DriverGetInfoRequest{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.AddContext(ctx).Errorf("health check failed, error is [%v]", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	log.AddContext(ctx).Infoln("health check successfully")
	return
}
