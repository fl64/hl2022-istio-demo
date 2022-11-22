package main

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/ilyakaznacheev/cleanenv"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

var BuildDatetime = "none"
var BuildVer = "devel"

type Cfg struct {
	SrvAddr    string        `env:"SERVER_ADDR" env-default:":8000"`
	PodName    string        `env:"POD_NAME"`
	PodNS      string        `env:"POD_NAMESPACE"`
	NodeName   string        `env:"NODE_NAME"`
	SleepDelay time.Duration `env:"SLEEP_DELAY" env-default:"1s"`
}

type App struct {
	cfg      Cfg
	srv      *http.Server
	disaster atomic.Bool
}

func NewApp(cfg Cfg) *App {
	return &App{
		cfg: cfg,
	}
}

func (a *App) Handler(w http.ResponseWriter, r *http.Request) {
	disaster := a.disaster.Load()
	log.Infof("Disaster annotation exists: %v", disaster)
	if disaster {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "NODE_NAME: %s — I feel bad...\n", a.cfg.NodeName)

	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "NODE_NAME: %s, POD_NAME: %s\n", a.cfg.NodeName, a.cfg.PodName)
	}
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
				pod, err := clientset.CoreV1().Pods(a.cfg.PodNS).Get(context.TODO(), a.cfg.PodName, metav1.GetOptions{})
				if err != nil {
					log.Errorf("Can't get pod: %v", err)
				}
				if metav1.HasAnnotation(pod.ObjectMeta, "disaster") {
					a.disaster.Store(true)
				} else {
					a.disaster.Store(false)
				}
				time.Sleep(3 * time.Second)
			}

		}()
	} else {
		log.Warn("Not in cluster")
	}

	r := mux.NewRouter()
	r.HandleFunc("/", a.Handler)

	a.srv = &http.Server{
		Addr:    a.cfg.SrvAddr,
		Handler: r,
	}

	j, _ := json.Marshal(a.cfg)
	log.Infof("Current config %s", string(j))

	log.Infof("Starting http on %s", a.cfg.SrvAddr)
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
