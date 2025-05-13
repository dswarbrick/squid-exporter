package collector

import (
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type descMap map[string]*prometheus.Desc

const (
	namespace = "squid"
	timeout   = 10 * time.Second
)

var (
	counters            descMap
	serviceTimes        descMap // ExtractServiceTimes decides if we want to extract service times
	ExtractServiceTimes bool
	infos               descMap
)

// Exporter entry point to Squid exporter.
type Exporter struct {
	client   SquidClient
	hostname string
	port     int
	up       *prometheus.GaugeVec
}

type CollectorConfig struct {
	Hostname    string
	Port        int
	Login       string
	Password    string
	Labels      prometheus.Labels
	ProxyHeader string
}

// New initializes a new exporter.
func New(c *CollectorConfig) *Exporter {
	counters = generateSquidCounters(c.Labels)
	if ExtractServiceTimes {
		serviceTimes = generateSquidServiceTimes(c.Labels)
	}

	infos = generateSquidInfos(c.Labels)

	return &Exporter{
		NewCacheObjectClient(fmt.Sprintf("http://%s:%d/squid-internal-mgr/", c.Hostname, c.Port), c.Login, c.Password, c.ProxyHeader),
		c.Hostname,
		c.Port,
		prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last query of squid successful?",
		}, []string{"host"}),
	}
}

// Describe describes all the metrics ever exported by the ECS exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.up.Describe(ch)

	for _, v := range counters {
		ch <- v
	}

	if ExtractServiceTimes {
		for _, v := range serviceTimes {
			ch <- v
		}
	}

	for _, v := range infos {
		ch <- v
	}
}

// Collect fetches metrics from Squid manager and expose them to Prometheus.
func (e *Exporter) Collect(c chan<- prometheus.Metric) {
	insts, err := e.client.GetCounters()

	if err == nil {
		e.up.With(prometheus.Labels{"host": e.hostname}).Set(1)
		for _, inst := range insts {
			if d, ok := counters[inst.Key]; ok {
				c <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, inst.Value)
			}
		}
	} else {
		e.up.With(prometheus.Labels{"host": e.hostname}).Set(0)
		log.Println("Could not fetch counter metrics from squid instance: ", err)
	}

	if ExtractServiceTimes {
		insts, err = e.client.GetServiceTimes()

		if err == nil {
			for _, inst := range insts {
				if d, ok := serviceTimes[inst.Key]; ok {
					c <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, inst.Value)
				}
			}
		} else {
			log.Println("Could not fetch service times metrics from squid instance: ", err)
		}
	}

	insts, err = e.client.GetInfos()
	if err == nil {
		for _, inst := range insts {
			if d, ok := infos[inst.Key]; ok {
				c <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, inst.Value)
			} else if inst.Key == "squid_info" {
				constLabels := make(prometheus.Labels)

				for _, vl := range inst.VarLabels {
					constLabels[vl.Key] = vl.Value
				}

				infoDesc := prometheus.NewDesc(
					prometheus.BuildFQName(namespace, "info", "service"),
					"Metrics as string from info on cache_object",
					nil, constLabels)
				c <- prometheus.MustNewConstMetric(infoDesc, prometheus.GaugeValue, inst.Value)
			}
		}
	} else {
		log.Println("Could not fetch info metrics from squid instance: ", err)
	}

	e.up.Collect(c)
}
