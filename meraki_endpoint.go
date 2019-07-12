package main

import (
  "runtime"
  "context"
  _ "expvar"
  "flag"
  "fmt"
  "sync/atomic"
  "log"
  "net/http"
  _ "net/http/pprof"
  "time"
  "os"
  "os/signal"
  "bytes"
  "io/ioutil"
  "github.com/json-iterator/go"
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/s3"
)
// Configure `jsoniter` to work as the `encoding/json` module API
var json = jsoniter.ConfigCompatibleWithStandardLibrary
// ScanData : Scanning API top level data
type ScanData struct {
	Type    string     `json:"type"`
	Secret  string     `json:"secret"`
	Version string     `json:"version"`
	Data    ClientData `json:"data"`
}
// ClientData : Client data
type ClientData struct {
	ApMac        string        `json:"apMac"`
	ApFloors     []string      `json:"apFloors"`
	ApTags       []string      `json:"apTags"`
	Observations []Observation `json:"observations"`
	Tenant       string        `json:"tenant"`
}
// Observation ; Observation data
type Observation struct {
	Ssid         string       `json:"ssid"`
	Ipv4         string       `json:"ipv4"`
	Ipv6         string       `json:"ipv6"`
	SeenEpoch    float64      `json:"seenEpoch"`
	SeenTime     string       `json:"seenTime"`
	Rssi         int          `json:"rssi"`
	Manufacturer string       `json:"manufacturer"`
	Os           string       `json:"os"`
	Location     LocationData `json:"location"`
	ClientMac    string       `json:"clientMac"`
}
// LocationData : Location Data
type LocationData struct {
	Lat float64   `json:"lat"`
	X   []float64 `json:"x"`
	Lng float64   `json:"lng"`
	Unc float64   `json:"unc"`
	Y   []float64 `json:"y"`
}

type job struct {
  scanData []byte
}

var (
  healthy int32
  logger *log.Logger
  loc *time.Location
  jobs chan job
	maxQueueSize = flag.Int("max_queue_size", 100, "The size of the job queue")
	maxWorkers = flag.Int("max_workers", 5, "The number of workers to start")
	port = flag.String("port", "8080", "The server port")
	bucket = flag.String("bucket", "cri.conatel.cloud", "The S3 Bucket where the data will be stored")
	location = flag.String("location", "UTC", "The time location")
	tls = flag.Bool("tls", false, "Should the server listen and serve tls")
	serverCrt = flag.String("server-tls", "server.crt", "Server TLS certificate")
	serverKey = flag.String("server-key", "server.key", "Server TLS key")
	validator = flag.String("validator", "da6a17c407bb11dfeec7392a5042be0a4cc034b6", "Meraki Sacnning API Validator")
  secret = flag.String("secret", "cjkww5rmn0001SE__2j7wztuy", "Meraki Sacnning API Secret")
  region = flag.String("region", "us-east-1", "AWS Region")
  pprofOn = flag.Bool("pprof-on", false, "Should a pprof server be run along the app")
)

func main() {
  /*
  Parse application flags
  */
	flag.Parse()
  /*
  Set the "Location" for the `time` module. UTC by default.
  */
	var err error
	loc, err = time.LoadLocation(*location)
	if err != nil {
		panic(err)
	}
  /*
  Create the logger
  */
  logger = log.New(os.Stdout, "http: ", log.LstdFlags)
  logger.Println("Server is starting...")
  /*
  Configure concurrent jobs by modifying the GOMAXPROCS env variable
  to match the ammount of CPUs on the server.
  */
  runtime.GOMAXPROCS(runtime.NumCPU())
  logger.Println("GOMAXPROCS =", os.Getenv("GOMAXPROCS"))
  logger.Println("maxWorkers =", *maxWorkers)
  logger.Println("maxQueueSize =", *maxQueueSize)
  logger.Println("port =", *port)
  /*
  Configure AWS Go SDK
  */
  sess, err := session.NewSession(&aws.Config{
    Region: aws.String(*region),
  })
  if err != nil {
    logger.Fatalf("Could not configure the AWS Go SDK: %v\n", err)
  }
  svc := s3.New(sess)
  /*
  Create the job queue. It creates a new channel to hold the jobs
  and starts all the workers.
  */
  jobs = make(chan job, *maxQueueSize)
  // Create the worker handler
  processBody := createProcessBody(svc, loc, *bucket, *secret)
  // Create workers
  for index := 1; index <= *maxWorkers; index++ {
    logger.Println("Starting worker #", index)
    go func(index int) {
      for job := range jobs {
        processBody(index, job)
      }
    }(index)
  }
  /*
  Run pprof server if the `pprofOn` flag is on
  */
  if *pprofOn == true {
    pprofMux := http.DefaultServeMux
    http.DefaultServeMux = http.NewServeMux()
    go func() {
      log.Println(http.ListenAndServe("localhost:6060", pprofMux))
    }()
  }
  /*
  Create and configure the API router
  */
  router := http.NewServeMux()
  router.Handle("/", handler())
  router.Handle("/healthz", healthz())
  /*
  Configure the HTTP server
  */
  server := &http.Server{
    Addr: "0.0.0.0:" + *port,
    Handler: logging()(router),
    ErrorLog: logger,
    ReadTimeout: 5 * time.Second,
    WriteTimeout: 10 * time.Second,
    IdleTimeout: 15 * time.Second,
  }
  /*
  Create the channels that respond to system signals
  */
  done := make(chan bool)
  quit := make(chan os.Signal, 1)
  signal.Notify(quit, os.Interrupt)
  /*
  Launch the server shutdown procedure on its own goroutine
  */
  go func() {
    <-quit
    logger.Println("Server is shutting down...")
    atomic.StoreInt32(&healthy, 0)
    ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
    defer cancel()
    server.SetKeepAlivesEnabled(false)
    if err := server.Shutdown(ctx); err != nil {
      logger.Fatalf("Could not gracefully shutdown the server: %v\n", err)
    }
    close(done)
  }()
  /*
  Run the HTTP or HTTPS server depending on the provided configuration
  */
  atomic.StoreInt32(&healthy, 1)
  if *tls == false {
    logger.Println("Server is ready to handle HTTP requests at", "0.0.0.0:" + *port)
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      logger.Fatalf("Could not listen on %s: %v\n", "0.0.0.0:" + *port, err)
    }
  } else {
    logger.Println("Server is ready to handle HTTPS requests at", "0.0.0.0:" + *port)
    if err := server.ListenAndServeTLS(*serverCrt, *serverKey); err != nil && err != http.ErrServerClosed {
      logger.Fatalf("Could not listen on %s: %v\n", "0.0.0.0:" + *port, err)
    }
  }
  /*
  Shutdown server message
  */
  <-done
  logger.Println("Server stopped")
}
/*
Creates the `processBody` function to handle the JSON data provided by
CISCO Meraki Scanning API. It provides access to the S3 service, time
location configuration, S3 Bucjet name, and Meraki secret through a 
closure.

The `processBody` function will be run by the job workers whenever a 
new `job` is inserted to the channel.
*/
func createProcessBody(svc *s3.S3, loc *time.Location, bucket string, secret string) func(id int, j job) {
  return func (id int, j job) {
    var scanData ScanData
    err := json.Unmarshal(j.scanData, &scanData)
    if err != nil {
      panic(err)
    }
    if secret != scanData.Secret {
      fmt.Println("Invalid secret", scanData.Secret)
      return
    }
    start := time.Now()
    data := scanData.Data
    now := time.Now().In(loc).Format(time.RFC3339)
    key := now + "-" + data.ApMac + ".json"
    data.Tenant = "tata"
    dataBytes, err := json.Marshal(data)
    if err != nil {
      panic(err)
    }
    input := &s3.PutObjectInput{
      Body: bytes.NewReader(dataBytes),
      Bucket: aws.String(bucket),
      Key: aws.String(key),
    }
    _, err = svc.PutObject(input)
    if err != nil {
      panic(err)
    }
    fmt.Println("Saved", key, "to S3 in", time.Since(start))
  }
}
/*
Health endpoint to use against ALB load balancers, or to check for the 
stability of the service.

It returns a 204 Code, not a 200 Code.
*/
func healthz() http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if atomic.LoadInt32(&healthy) == 1 {
      w.WriteHeader(http.StatusNoContent)
      return
    }
    w.WriteHeader(http.StatusServiceUnavailable)
  })
}
/*
Handles request to the `/` endpoint. Meraki expects a GET request to `/`
to return the validator for the organization, and a POST to `/` to handle
the JSON information.
*/
func handler() http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
      getValidator(w, r)
      return
    }
    if r.Method == http.MethodPost {
      postData(w, r, jobs)
      return
    }
    http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
  })
}
/*
Returns the organization validator number
*/
func getValidator(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Content-Type", "text/plain; charset=utf-8")
  w.WriteHeader(http.StatusOK)
  fmt.Fprintln(w, *validator)
}
/*
Reads the body to a []byte and creates a new `job` with it. Then it returns
with a 202 success code.
*/
func postData(w http.ResponseWriter, r *http.Request, jobs chan job) {
  scanData, err := ioutil.ReadAll(r.Body)
  if err != nil {
    w.WriteHeader(http.StatusAccepted)
    fmt.Println(err)
    return
  }
  go func() {
    jobs <- job{scanData}
  }()
  w.WriteHeader(http.StatusAccepted)
}
/*
Middleware loggin function. Waits for the request handler to finish processing the
request before printing a log message.
*/
func logging() func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      start := time.Now()
      defer func() {
        logger.Println(r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent(), time.Since(start),)
      }()
      next.ServeHTTP(w, r)
    })
  }
}