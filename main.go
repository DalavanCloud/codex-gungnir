/**
 * Copyright 2019 Comcast Cable Communications Management, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"fmt"

	"github.com/goph/emperror"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"os"
	"os/signal"
	"time"

	"github.com/Comcast/codex/db"
	"github.com/Comcast/webpa-common/bookkeeping"
	"github.com/Comcast/webpa-common/concurrent"
	"github.com/Comcast/webpa-common/logging"
	"github.com/Comcast/webpa-common/secure/handler"
	"github.com/Comcast/webpa-common/server"
)

const (
	applicationName, apiBase = "gungnir", "/api/v1"
	DEFAULT_KEY_ID           = "current"
	applicationVersion       = "0.0.0"
)

type Config struct {
	Db            db.Config
	GetRetries    int
	RetryInterval time.Duration
}

func gungnir(arguments []string) int {
	start := time.Now()

	var (
		f, v                                = pflag.NewFlagSet(applicationName, pflag.ContinueOnError), viper.New()
		logger, metricsRegistry, codex, err = server.Initialize(applicationName, arguments, f, v)
	)

	printVer := f.BoolP("version", "v", false, "displays the version number")
	if err := f.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse arguments: %s\n", err.Error())
		return 1
	}

	if *printVer {
		fmt.Println(applicationVersion)
		return 0
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to initialize viper: %s\n", err.Error())
		return 1
	}
	logging.Info(logger).Log(logging.MessageKey(), "Successfully loaded config file", "configurationFile", v.ConfigFileUsed())

	// add GetValidator function (originally from caduceus)
	//validator, err := server.GetValidator(v, DEFAULT_KEY_ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Validator error: %v\n", err)
		return 1
	}

	config := new(Config)

	v.Unmarshal(config)
	dbConfig := config.Db

	//vaultClient, err := xvault.Initialize(v)
	//if err != nil {
	//	fmt.Fprintf(os.Stderr, "Vauilt Initialize error: %v\n", err)
	//	return 3
	//}
	//usr, pwd := vaultClient.GetUsernamePassword("dev", "couchbase")
	//if usr == "" || pwd == "" {
	//	fmt.Fprintf(os.Stderr, "Failed to get Login credientals to couchbase")
	//	return 3
	//}
	//database.Username = usr
	//database.Password = pwd

	database, err := db.CreateDbConnection(dbConfig)
	if err != nil {
		logging.Error(logger, emperror.Context(err)...).Log(logging.MessageKey(), "Failed to initialize database connection",
			logging.ErrorKey(), err.Error())
		fmt.Fprintf(os.Stderr, "Database Initialize Failed: %#v\n", err)
		return 2
	}
	hg := db.CreateRetryHGService(database, config.GetRetries, config.RetryInterval)
	tg := db.CreateRetryTGService(database, config.GetRetries, config.RetryInterval)

	authHandler := handler.AuthorizationHandler{
		HeaderName:          "Authorization",
		ForbiddenStatusCode: 403,
		//Validator:           validator,
		Logger: logger,
	}
	// TODO: fix bookkeeping, add a decorator to add the bookkeeping requests and logger
	bookkeeper := bookkeeping.New(bookkeeping.WithResponses(bookkeeping.Code))

	gungnirHandler := alice.New(authHandler.Decorate, bookkeeper)
	router := mux.NewRouter()
	// MARK: Actual server logic
	app := &App{
		historyGetter:   hg,
		tombstoneGetter: tg,
		logger:          logger,
	}

	router.Handle(apiBase+"/device/{deviceID}", gungnirHandler.ThenFunc(app.handleGetAll))
	// router.Handle(apiBase+"/device/{deviceID}/last", gungnirHandler.ThenFunc(app.handleGetLastState))

	// MARK: Starting the server
	_, runnable, done := codex.Prepare(logger, nil, metricsRegistry, router)

	waitGroup, shutdown, err := concurrent.Execute(runnable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to start device manager: %s\n", err)
		return 1
	}

	logging.Info(logger).Log(logging.MessageKey(), fmt.Sprintf("%s is up and running!", applicationName), "elapsedTime", time.Since(start))
	signals := make(chan os.Signal, 10)
	signal.Notify(signals)
	for exit := false; !exit; {
		select {
		case s := <-signals:
			if s != os.Kill && s != os.Interrupt {
				logging.Info(logger).Log(logging.MessageKey(), "ignoring signal", "signal", s)
			} else {
				logging.Error(logger).Log(logging.MessageKey(), "exiting due to signal", "signal", s)
				exit = true
			}
		case <-done:
			logging.Error(logger).Log(logging.MessageKey(), "one or more servers exited")
			exit = true
		}
	}

	close(shutdown)
	waitGroup.Wait()
	return 0
}

func main() {
	os.Exit(gungnir(os.Args))
}
