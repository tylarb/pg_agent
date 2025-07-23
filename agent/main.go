package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jackc/pgx/v5/pgxpool"
)

// exported metrics
var (
	// counter, total inserted tuples since pg runtime or stats reset
	insertedTupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pg_tup_inserted_total",
		Help: "Number of rows inserted by queries in this database",
	},
		[]string{"datid", "datname"})

	// gauge, inserted since last scrape
	insertedTup = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pg_tup_inserted",
		Help: "Guage, number of rows inserted by queries in this database",
	},
		[]string{"datid", "datname"})

	// counter, total updated tuples since pg runtime or stats reset
	updatedTupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pg_tup_updated_total",
		Help: "Number of rows updated by queries in this database",
	},
		[]string{"datid", "datname"})

	// gauge, updated since last scrape
	updatedTup = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pg_tup_updated",
		Help: "Guage, number of rows updated by queries in this database",
	},
		[]string{"datid", "datname"})
)

// pg scan interval and settings
// for the purposes of this test, setting this 10s allows plenty of time to run
// some benchmark where we see the metric increment
// but such a high value would cause a lot of averaging and smoothing in a graph, making it
// hard to pinpoint small window events
var (
	sleep_interval_ms = 10000
)

var (
	pgpool *pgxpool.Pool
)

func main() {
	fmt.Println("Starting agent")

	// leave metrics connection open
	// url = "postgres://username:password@[hostname]:port/db_name"
	var err error
	pgpool, err = pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))

	if err != nil {
		fmt.Printf("ERROR: Could not connect to DB with error: %v\n", err)
		os.Exit(1)
	}
	defer pgpool.Close()

	fmt.Println("Connected to DB successfully")

	fmt.Println("Starting metrics collection. Metrics are available at localhost:5480/metrics")
	recordMetrics()

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":5480", nil)

}

func recordMetrics() {

	// internal counters
	// map from datit to inserted or updated
	// TODO: this could be better structured
	localCountInserted := make(map[int]int)
	localCountUpdated := make(map[int]int)

	// results from DB query
	var (
		datid        int
		datname      string
		tup_inserted int
		tup_updated  int
	)
	// TODO: this would be good candidate to move to a subroutine
	go func() {
		for {
			// there's going to be interplay between this time duration and the prometheus scrape interval
			time.Sleep(time.Duration(sleep_interval_ms) * time.Millisecond)
			// TODO the casting is a bit of a hack
			rows, err := pgpool.Query(context.Background(), "select datid::int, datname, tup_inserted, tup_updated from pg_stat_database where datname is not NULL")
			if err != nil {
				fmt.Printf("ERROR: unable to query pg_stat_database: %v\n", err)
			}
			defer rows.Close()
			for rows.Next() {
				err = rows.Scan(&datid, &datname, &tup_inserted, &tup_updated)
				if err != nil {
					fmt.Printf("ERROR:error scanning rows: %v\n", err)
					os.Exit(1)
				}
				var (
					shipInserted, shipUpdated int
				)
				if tup_inserted < localCountInserted[datid] || tup_updated < localCountUpdated[datid] {
					shipInserted = tup_inserted
					shipUpdated = tup_updated
				} else {
					shipInserted = tup_inserted - localCountInserted[datid]
					shipUpdated = tup_updated - localCountUpdated[datid]
				}

				localCountInserted[datid] = tup_inserted
				localCountUpdated[datid] = tup_updated

				insertedTupTotal.WithLabelValues(strconv.Itoa(datid), datname).Add(float64(shipInserted))
				updatedTupTotal.WithLabelValues(strconv.Itoa(datid), datname).Add(float64(shipUpdated))
				insertedTup.WithLabelValues(strconv.Itoa(datid), datname).Set(float64(shipInserted))
				updatedTup.WithLabelValues(strconv.Itoa(datid), datname).Set(float64(shipUpdated))

			}
		}
	}()

}
