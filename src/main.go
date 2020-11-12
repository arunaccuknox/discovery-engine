package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/accuknox/knoxAutoPolicy/src/core"
	"github.com/accuknox/knoxAutoPolicy/src/libs"
	"github.com/accuknox/knoxAutoPolicy/src/types"

	"gopkg.in/yaml.v2"

	cron "github.com/robfig/cron/v3"
)

// network flow between [ startTime <= time < endTime ]
var startTime int64 = 0
var endTime int64 = 0

var configFile string

func loadConfiguration() types.Config {
	f, err := os.Open(configFile)
	if err != nil {
		fmt.Println(err)
	}
	defer f.Close()

	var cfg types.Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		fmt.Println(err)
	}

	return cfg
}

func init() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./knoxAutoPolicy -config=[configfile]")
		os.Exit(1)
	}

	flag.StringVar(&configFile, "config", "config.yaml", "Config file")
	flag.Parse()

	// init time filter
	endTime = time.Now().Unix()
	startTime = 0
}

// Generate function
func Generate() {
	cfg := loadConfiguration()

	// get network traffic from  knox aggregation Databse
	trafficList, err := libs.GetTrafficFlowByTime(cfg, startTime, endTime)
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(trafficList) < 1 {
		fmt.Println("Traffic flow is not exist: ",
			time.Unix(startTime, 0).Format(libs.TimeFormSimple), " ~ ",
			time.Unix(endTime, 0).Format(libs.TimeFormSimple))

		startTime = endTime
		endTime = time.Now().Unix()
		return
	}

	// time filter update for next interval
	startTime = trafficList[len(trafficList)-1].TrafficFlow.Time + 1
	endTime = time.Now().Unix()

	fmt.Println("the total number of traffic flow from db: ", len(trafficList))

	// get all the namespaces from k8s
	namespaces := libs.K8s.GetK8sNamespaces()
	for _, namespace := range namespaces {
		if cfg.Policy.Namespace != namespace {
			continue
		}

		fmt.Println("policy discovery started for namespace: ", namespace)

		// convert network traffic -> network log, and filter traffic
		networkLogs := libs.ConvertTrafficFlowToLogs(namespace, trafficList)

		// get k8s services
		services := libs.K8s.GetServices(namespace)

		// get k8s endpoints
		endpoints := libs.K8s.GetEndpoints(namespace)

		// get pod information
		pods := libs.K8s.GetConGroups(namespace)

		// generate network policies
		policies := core.GenerateNetworkPolicies(namespace, cfg.Policy.CidrBits, networkLogs, services, endpoints, pods)

		if len(policies) > 0 {
			// write discovered policies to files
			libs.WriteCiliumPolicyToFile(namespace, policies)

			// insert discovered policies to db
			libs.InsertDiscoveredPolicies(cfg, policies)
		}

		fmt.Println("policy discovery done for namespace: ", namespace, " ", len(policies))
	}
}

// CronJobDaemon function
func CronJobDaemon() {
	// init cron job
	c := cron.New()
	c.AddFunc("@every 0h0m30s", Generate) // every time interval for test
	c.Start()

	sig := libs.GetOSSigChannel()
	<-sig
	println("Got a signal to terminate the auto policy discovery")

	c.Stop() // Stop the scheduler (does not stop any jobs already running).
}

func main() {
	Generate()
}
