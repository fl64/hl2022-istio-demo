package main

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type App struct {
	addr        string
	podName     string
	podNS       string
	clusterName string
	srv         *http.Server
	disaster    atomic.Bool
}

func NewApp(addr, name, ns, cluster string) *App {
	return &App{
		addr:        addr,
		podName:     name,
		podNS:       ns,
		clusterName: cluster,
	}
}

func (a *App) Handler(w http.ResponseWriter, r *http.Request) {
	disaster := a.disaster.Load()
	log.Infof("Disaster annotation exists: %v", disaster)
	if disaster {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "CLUSTER_NAME: %s — I feel bad...\n", a.clusterName)

	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "CLUSTER_NAME: %s, POD_NAME: %s\n", a.clusterName, a.podName)
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
				pod, err := clientset.CoreV1().Pods(a.podNS).Get(context.TODO(), a.podName, metav1.GetOptions{})
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
		Addr:    a.addr,
		Handler: r,
	}

	log.Infof("Starting http on %s", a.addr)
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

	a := NewApp(":8000", os.Getenv("POD_NAME"), os.Getenv("POD_NAMESPACE"), os.Getenv("CLUSTER_NAME"))

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

	err := a.Run(ctx)
	if err != nil {
		log.Fatalf("Can't run app: %v \n", err)
	}
}
