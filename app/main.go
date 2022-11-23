package main

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/ilyakaznacheev/cleanenv"
	log "github.com/sirupsen/logrus"
	"html/template"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const htmlTemplate = `
{{ $ElementFormat := "[ %s ] %d %s" }}
<html>
  <head>
    {{ if .Cfg.AutoRefresh }}
    <meta http-equiv="refresh" content="{{ .Cfg.AutoInterval }}">
    {{ end }}
  </head>
  <body style="font-family: monospace">
   <p>
     <b>Frontend</b>
     <p>
       <span style="color: black">
         {{ printf "%s %s" .Cfg.PodName .Cfg.NodeName }}
       </span>
     </p>
   </p>
   <p>
     <b>Backend</b>
     <p>
       {{ if eq .State.Last.Status 200 }} 
       <span style="color: green">
       {{ else }}
       <span style="color: red">
       {{ end }}
         {{ printf $ElementFormat .State.Last.TimeStamp .State.Last.Status .State.Last.Body }}
       </span>
     </p>
   </p>
   {{ if .State.History }}
   <p>
     <b>History (Last {{ .Cfg.HistorySize }})</b>
     {{ range $index, $element := .State.History }}
     {{ if ge $index $.Cfg.HistorySize }} {{ break }} {{ end }} 
     <p>
       {{ if eq $element.Status 200 }} 
       <span style="color: green">
       {{ else }}
       <span style="color: red">
       {{ end }}
         {{ printf $ElementFormat $element.TimeStamp $element.Status $element.Body }}
       </span>
     </p>
     {{end}}
   </p>
   {{ end }}
  </body>
</html>
`

var BuildDatetime = "none"
var BuildVer = "devel"

type Cfg struct {
	SrvAddr      string        `env:"SERVER_ADDR" env-default:":8000"`
	BackendAddr  string        `env:"BACKEND_ADDR"`
	PodName      string        `env:"POD_NAME"`
	PodNS        string        `env:"POD_NAMESPACE"`
	NodeName     string        `env:"NODE_NAME"`
	SleepDelay   time.Duration `env:"SLEEP_DELAY" env-default:"1s"`
	HistorySize  int           `env:"HISTORY_SIZE" env-default:"20"`
	TimeFormat   string        `env:"TIME_FORMAT" env-default:"15:04:05.999"`
	AutoRefresh  bool          `env:"AUTOREFRESH_ENABLE" env-default:"true"`
	AutoInterval int           `env:"AUTOREFRESH_INTERVAL" env-default:"1"`
}

type HistoryItem struct {
	TimeStamp string
	Body      string
	Status    int
}

type State struct {
	Last    HistoryItem
	History []HistoryItem
}

type App struct {
	Cfg      Cfg
	srv      *http.Server
	client   *http.Client
	disaster atomic.Bool
	State    State
}

func NewApp(cfg Cfg) *App {
	return &App{
		Cfg:    cfg,
		client: &http.Client{},
		State: State{
			Last:    HistoryItem{},
			History: make([]HistoryItem, 0),
		},
	}
}

func (a *App) BackendHandler(w http.ResponseWriter, r *http.Request) {
	disaster := a.disaster.Load()
	log.Infof("Disaster annotation exists: %v", disaster)
	if disaster {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s — I feel bad...\n", a.Cfg.NodeName)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%s %s\n", a.Cfg.PodName, a.Cfg.NodeName)
	}
}

func (a *App) FrontendHandler(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf("%s/%s", a.Cfg.BackendAddr, r.URL)
	req, err := http.NewRequest("GET", url, r.Body)
	if err != nil {
		log.Errorf("Can't create newrequest", err)
		return
	}

	// Add all X- headers to request
	for name, values := range r.Header {
		// Loop over all values for the name.
		for _, value := range values {
			if strings.HasPrefix(name, "X-") {
				req.Header.Set(name, value)
			}
		}
	}

	resp, err := a.client.Do(req)
	if err != nil {
		log.Errorf("Can't do request", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}

	log.Infof("Got response form backend '%s' with status '%s'", a.Cfg.BackendAddr, resp.Status)
	a.State.Last.Status = resp.StatusCode
	a.State.Last.TimeStamp = time.Now().Format(a.Cfg.TimeFormat) // "2006-01-02 15:04:05.999"
	a.State.Last.Body = string(body)

	w.WriteHeader(http.StatusOK)

	tmpl := template.New("tpl")
	tmpl, err = tmpl.Parse(htmlTemplate)
	if err != nil {
		log.Errorf("Can't parse template: %v", err)
	}
	err = tmpl.Execute(w, a)
	if err != nil {
		log.Errorf("Can't execute template: %v", err)
	}

	lim := len(a.State.History)
	if lim > a.Cfg.HistorySize {
		lim = a.Cfg.HistorySize
	}

	old := a.State.History[:lim]
	a.State.History = append([]HistoryItem{}, a.State.Last)
	a.State.History = append(a.State.History, old...)
}

func (a *App) Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t1 := time.Now()
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
		t2 := time.Now()
		log.WithFields(log.Fields{
			"url":            r.URL.String(),
			"host":           r.Host,
			"user-agent":     r.Header["User-Agent"],
			"method":         r.Method,
			"remote-addr":    r.RemoteAddr,
			"content-length": r.ContentLength,
			"duration":       t2.Sub(t1).String(),
		}).Info()
	})
}

func (a *App) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		ctxShutDown := context.Background()
		ctxShutDown, cancel := context.WithTimeout(ctxShutDown, time.Second*5)
		defer func() {
			cancel()
		}()
		if err := a.srv.Shutdown(ctxShutDown); err != nil {
			log.Fatalf("http server Shutdown Failed:%s", err)
		}
	}()

	// watching for annotations
	//a.disaster.Store(false)
	cfg, err := rest.InClusterConfig()
	if err == nil {
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			log.Fatalf("Error building kubernetes clientset: %v", err)
		}
		go func() {
			log.Infof("Run annotation checker")
			for {
				pod, err := clientset.CoreV1().Pods(a.Cfg.PodNS).Get(context.TODO(), a.Cfg.PodName, metav1.GetOptions{})
				if err != nil {
					log.Errorf("Can't get pod: %v", err)
				}
				if metav1.HasAnnotation(pod.ObjectMeta, "disaster") {
					a.disaster.Store(true)
				} else {
					a.disaster.Store(false)
				}
				time.Sleep(a.Cfg.SleepDelay)
			}

		}()
	} else {
		log.Warn("Not in cluster")
	}

	r := mux.NewRouter()
	r.Use(a.Logging)
	if a.Cfg.BackendAddr == "" {
		r.PathPrefix("/").HandlerFunc(a.BackendHandler)
	} else {
		r.PathPrefix("/").HandlerFunc(a.FrontendHandler)
	}

	a.srv = &http.Server{
		Addr:    a.Cfg.SrvAddr,
		Handler: r,
	}

	j, _ := json.Marshal(a.Cfg)
	log.Infof("Current config %s", string(j))

	log.Infof("Starting http on %s", a.Cfg.SrvAddr)
	err = a.srv.ListenAndServe()

	if err != nil && err != http.ErrServerClosed {
		return err
	}
	log.Info("App stopped")
	return nil
}

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.DebugLevel)
	log.Infof("App ver '%s', build time '%s'", BuildVer, BuildDatetime)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var cfg Cfg
	err := cleanenv.ReadEnv(&cfg)
	if err != nil {
		log.Fatalf("Can't load env")
	}
	a := NewApp(cfg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(
		sigChan,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	go func() {
		s := <-sigChan
		close(sigChan)
		log.Println("Catch signal: ", s)
		cancel()
	}()

	err = a.Run(ctx)
	if err != nil {
		log.Fatalf("Can't run app: %v \n", err)
	}
}
