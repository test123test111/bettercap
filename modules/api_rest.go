package modules

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bettercap/bettercap/core"
	"github.com/bettercap/bettercap/log"
	"github.com/bettercap/bettercap/session"
	"github.com/bettercap/bettercap/tls"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type RestAPI struct {
	session.SessionModule
	server       *http.Server
	username     string
	password     string
	certFile     string
	keyFile      string
	allowOrigin  string
	useWebsocket bool
	upgrader     websocket.Upgrader
	quit         chan bool
}

func NewRestAPI(s *session.Session) *RestAPI {
	api := &RestAPI{
		SessionModule: session.NewSessionModule("api.rest", s),
		server:        &http.Server{},
		quit:          make(chan bool),
		useWebsocket:  false,
		allowOrigin:   "*",
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}

	api.AddParam(session.NewStringParameter("api.rest.address",
		session.ParamIfaceAddress,
		session.IPv4Validator,
		"Address to bind the API REST server to."))

	api.AddParam(session.NewIntParameter("api.rest.port",
		"8081",
		"Port to bind the API REST server to."))

	api.AddParam(session.NewStringParameter("api.rest.alloworigin",
		api.allowOrigin,
		"",
		"Value of the Access-Control-Allow-Origin header of the API server."))

	api.AddParam(session.NewStringParameter("api.rest.username",
		"",
		"",
		"API authentication username."))

	api.AddParam(session.NewStringParameter("api.rest.password",
		"",
		"",
		"API authentication password."))

	api.AddParam(session.NewStringParameter("api.rest.certificate",
		"",
		"",
		"API TLS certificate."))

	tls.CertConfigToModule("api.rest", &api.SessionModule, tls.DefaultLegitConfig)

	api.AddParam(session.NewStringParameter("api.rest.key",
		"",
		"",
		"API TLS key"))

	api.AddParam(session.NewBoolParameter("api.rest.websocket",
		"false",
		"If true the /api/events route will be available as a websocket endpoint instead of HTTPS."))

	api.AddHandler(session.NewModuleHandler("api.rest on", "",
		"Start REST API server.",
		func(args []string) error {
			return api.Start()
		}))

	api.AddHandler(session.NewModuleHandler("api.rest off", "",
		"Stop REST API server.",
		func(args []string) error {
			return api.Stop()
		}))

	return api
}

type JSSessionRequest struct {
	Command string `json:"cmd"`
}

type JSSessionResponse struct {
	Error string `json:"error"`
}

func (api *RestAPI) Name() string {
	return "api.rest"
}

func (api *RestAPI) Description() string {
	return "Expose a RESTful API."
}

func (api *RestAPI) Author() string {
	return "Simone Margaritelli <evilsocket@protonmail.com>"
}

func (api *RestAPI) isTLS() bool {
	return api.certFile != "" && api.keyFile != ""
}

func (api *RestAPI) Configure() error {
	var err error
	var ip string
	var port int

	if api.Running() {
		return session.ErrAlreadyStarted
	} else if err, ip = api.StringParam("api.rest.address"); err != nil {
		return err
	} else if err, port = api.IntParam("api.rest.port"); err != nil {
		return err
	} else if err, api.allowOrigin = api.StringParam("api.rest.alloworigin"); err != nil {
		return err
	} else if err, api.certFile = api.StringParam("api.rest.certificate"); err != nil {
		return err
	} else if api.certFile, err = core.ExpandPath(api.certFile); err != nil {
		return err
	} else if err, api.keyFile = api.StringParam("api.rest.key"); err != nil {
		return err
	} else if api.keyFile, err = core.ExpandPath(api.keyFile); err != nil {
		return err
	} else if err, api.username = api.StringParam("api.rest.username"); err != nil {
		return err
	} else if err, api.password = api.StringParam("api.rest.password"); err != nil {
		return err
	} else if err, api.useWebsocket = api.BoolParam("api.rest.websocket"); err != nil {
		return err
	}

	if api.isTLS() {
		if !core.Exists(api.certFile) || !core.Exists(api.keyFile) {
			err, cfg := tls.CertConfigFromModule("api.rest", api.SessionModule)
			if err != nil {
				return err
			}

			log.Debug("%+v", cfg)
			log.Info("generating TLS key to %s", api.keyFile)
			log.Info("generating TLS certificate to %s", api.certFile)
			if err := tls.Generate(cfg, api.certFile, api.keyFile); err != nil {
				return err
			}
		} else {
			log.Info("loading TLS key from %s", api.keyFile)
			log.Info("loading TLS certificate from %s", api.certFile)
		}
	}

	api.server.Addr = fmt.Sprintf("%s:%d", ip, port)

	router := mux.NewRouter()

	router.HandleFunc("/api/events", api.eventsRoute)
	router.HandleFunc("/api/session", api.sessionRoute)
	router.HandleFunc("/api/session/ble", api.sessionRoute)
	router.HandleFunc("/api/session/ble/{mac}", api.sessionRoute)
	router.HandleFunc("/api/session/env", api.sessionRoute)
	router.HandleFunc("/api/session/gateway", api.sessionRoute)
	router.HandleFunc("/api/session/interface", api.sessionRoute)
	router.HandleFunc("/api/session/lan", api.sessionRoute)
	router.HandleFunc("/api/session/lan/{mac}", api.sessionRoute)
	router.HandleFunc("/api/session/options", api.sessionRoute)
	router.HandleFunc("/api/session/packets", api.sessionRoute)
	router.HandleFunc("/api/session/started-at", api.sessionRoute)
	router.HandleFunc("/api/session/wifi", api.sessionRoute)
	router.HandleFunc("/api/session/wifi/{mac}", api.sessionRoute)

	api.server.Handler = router

	if api.username == "" || api.password == "" {
		log.Warning("api.rest.username and/or api.rest.password parameters are empty, authentication is disabled.")
	}

	return nil
}

func (api *RestAPI) Start() error {
	if err := api.Configure(); err != nil {
		return err
	}

	api.SetRunning(true, func() {
		var err error

		if api.isTLS() {
			log.Info("api server starting on https://%s", api.server.Addr)
			err = api.server.ListenAndServeTLS(api.certFile, api.keyFile)
		} else {
			log.Info("api server starting on http://%s", api.server.Addr)
			err = api.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	})

	return nil
}

func (api *RestAPI) Stop() error {
	return api.SetRunning(false, func() {
		go func() {
			api.quit <- true
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		api.server.Shutdown(ctx)
	})
}
