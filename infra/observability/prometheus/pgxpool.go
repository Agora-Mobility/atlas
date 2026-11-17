package prometheus

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// RegisterPool exposes the statistics of the database pool. A pool that
// waits for connections (empty acquires) is the first sign of saturation,
// before requests start to time out.
func RegisterPool(pool *pgxpool.Pool) error {
	err := prometheus.Register(newPoolCollector(pool))
	var already prometheus.AlreadyRegisteredError
	if errors.As(err, &already) {
		return nil
	}
	return err
}

type poolCollector struct {
	pool *pgxpool.Pool

	maxConns, totalConns, acquiredConns, idleConns *prometheus.Desc
	acquires, emptyAcquires, canceledAcquires      *prometheus.Desc
	acquireSeconds                                 *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "db_pool", name), help, nil, nil)
	}
	return &poolCollector{
		pool:             pool,
		maxConns:         desc("max_conns", "Maximum size of the pool."),
		totalConns:       desc("conns", "Connections currently open."),
		acquiredConns:    desc("acquired_conns", "Connections currently in use."),
		idleConns:        desc("idle_conns", "Connections open and idle."),
		acquires:         desc("acquires_total", "Successful connection acquires."),
		emptyAcquires:    desc("empty_acquires_total", "Acquires that had to wait because the pool was empty."),
		canceledAcquires: desc("canceled_acquires_total", "Acquires canceled before a connection was available."),
		acquireSeconds:   desc("acquire_seconds_total", "Total time spent acquiring connections."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.maxConns, c.totalConns, c.acquiredConns, c.idleConns,
		c.acquires, c.emptyAcquires, c.canceledAcquires, c.acquireSeconds,
	} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.emptyAcquires, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquires, prometheus.CounterValue, float64(s.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireSeconds, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
