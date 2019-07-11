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

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// Location : Meraki device observation location
type Location struct {
  Lat float64 `json:"lat,omitempty"`
  Lng float64 `json:"lng,omitempty"`
  Unc float64 `json:"unc,omitempty"`
  X []float64 `json:"x,omitempty"`
  Y []float64 `json:"y,omitempty"`
}
// Observation : Meraki device observation
type Observation struct {
  IPv4 string `json:"ipv4,omitempty"`
  Location Location `json:"location,omitempty"`
  SeenTime string `json:"seenTime,omitempty"`
  SSID string `json:"ssid,omitempty"`
  OS string `json:"os,omitempty"`
  ClientMac string `json:"clientMac,omitempty"`
  SeenEpoch int64 `json:"seenEpoch,omitempty"`
  RSSI int `json:"rssi,omitempty"`
  IPv6 string `json:"ipv6,omitempty"`
  Manufacturer string `json:"manufacturer,omitempty"`
}
// DevicesSeenData : Meraki devices seen data
type DevicesSeenData struct {
  ApMac string `json:"apMac,omitempty"`
  ApTags []string `json:"apTags,omitempty"`
  ApFloors []string `json:"apFloors,omitempty"`
  Observations []Observation `json:"observations,omitempty"`
  Tenant string `json:"tenant,omitempty"`   
}
// DevicesSeen : Meraki devices seen
type DevicesSeen struct {
  Version string `json:"version,omitempty"`
  Secret string `json:"secret,omitempty"`
  Type string `json:"type,omitempty"`
  Data DevicesSeenData  `json:"data,omitempty"`
}

type job struct {
  devicesSeen []byte
}

var (
  healthy int32
  logger *log.Logger
)

func createProcessBody(svc *s3.S3, loc *time.Location, bucket string, secret string) func(id int, j job) {
  return func (id int, j job) {
    var devicesSeen DevicesSeen
    err := json.Unmarshal(j.devicesSeen, &devicesSeen)
    if err != nil {
      panic(err)
    }
    if secret != devicesSeen.Secret {
      fmt.Println("Invalid secret", devicesSeen.Secret)
      return
    }
    start := time.Now()
    data := devicesSeen.Data
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

func main() {
  var (
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
  )
  flag.Parse()
  // Set the time location
  loc, err := time.LoadLocation(*location)
  if err != nil {
    panic(err)
  }
  // Logger configuration
  logger := log.New(os.Stdout, "http: ", log.LstdFlags)
  logger.Println("Server is starting...")
  // Configure concurrent jobs
  runtime.GOMAXPROCS(runtime.NumCPU())
  logger.Println("GOMAXPROCS =", runtime.NumCPU())
  logger.Println("maxWorkers =", *maxWorkers)
  logger.Println("maxQueueSize =", *maxQueueSize)
  logger.Println("port =", *port)
  // Create an AWS Session and an s3 service
  sess, err := session.NewSession(&aws.Config{
    Region: aws.String("us-east-1"),
  })
  if err != nil {
    logger.Fatalf("Could not configure the AWS Go SDK: %v\n", err)
  }
  svc := s3.New(sess)
  // Create job channel
  jobs := make(chan job, *maxQueueSize)
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
  // Router configuration
  router := http.NewServeMux()
  router.Handle("/", handler(*validator, jobs))
  router.Handle("/healthz", healthz())
  // Server configuration
  server := &http.Server{
    Addr: "0.0.0.0:" + *port,
    Handler: logging(logger)(router),
    ErrorLog: logger,
    ReadTimeout: 5 * time.Second,
    WriteTimeout: 10 * time.Second,
    IdleTimeout: 15 * time.Second,
  }
  // Configure done and quit handlers
  done := make(chan bool)
  quit := make(chan os.Signal, 1)
  signal.Notify(quit, os.Interrupt)
  // Launch quit handler goroutine
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
  // Run server
  logger.Println("Server is ready to handle requests at", "0.0.0.0:" + *port)
  atomic.StoreInt32(&healthy, 1)
  if *tls == false {
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      logger.Fatalf("Could not listen on %s: %v\n", "0.0.0.0:" + *port, err)
    }
  } else {
    if err := server.ListenAndServeTLS(*serverCrt, *serverKey); err != nil && err != http.ErrServerClosed {
      logger.Fatalf("Could not listen on %s: %v\n", "0.0.0.0:" + *port, err)
    }
  }
  // Handle done
  <-done
  logger.Println("Server stopped")
}

func healthz() http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if atomic.LoadInt32(&healthy) == 1 {
      w.WriteHeader(http.StatusNoContent)
      return
    }
    w.WriteHeader(http.StatusServiceUnavailable)
  })
}

func handler(validator string, jobs chan job) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet && r.Method != http.MethodPost {
      http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
      return
    }
    if r.Method == http.MethodGet {
      sendValidator(w, r, validator)
      return
    }
    if r.Method == http.MethodPost {
      handleData(w, r, jobs)
      return
    }
  })
}

func sendValidator(w http.ResponseWriter, r *http.Request, validator string) {
  w.Header().Set("Content-Type", "text/plain; charset=utf-8")
  w.Header().Set("X-Content-Type-Options", "nosniff")
  w.WriteHeader(http.StatusOK)
  fmt.Fprintln(w, validator)
}

func handleData(w http.ResponseWriter, r *http.Request, jobs chan job) {
  //var devicesSeen DevicesSeen
  //err := json.NewDecoder(r.Body).Decode(&devicesSeen)
  devicesSeen, err := ioutil.ReadAll(r.Body)
  if err != nil {
   http.Error(w, "Bad request - Can't Decode!", 400)
   panic(err)
  }
  // Create Job and push the work into the Job Channel
  go func() {
    jobs <- job{devicesSeen}
  }()
  // Render success
  w.WriteHeader(http.StatusAccepted)
}

func logging(logger *log.Logger) func(http.Handler) http.Handler {
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