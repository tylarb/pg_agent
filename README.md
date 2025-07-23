# Sample postgres prometheus agent

## prereqs

- Container running postgres
- pg_bench
- psql (which can be run in the container, if necessary)
- golang

## Background and problem statement

postgres provides a counter of tuples updated and inserted per database. We want these tracked both as a counter and a guage.
Counters increase forever (like the pg_stats table), while guages report a varaible number. In this case, we decide that the guage will report the number of updates or inserts since the last scrape interval


This code is an agent that provides a prometheus metrics endpoint that provides a counter and a guage for inserted and updated records, which also tags the metrics with datid and datname for filtering in prometheus

[See documentation on counters vs guages](https://prometheus.io/docs/concepts/metric_types/)

```
postgres=# select datid, datname, tup_inserted, tup_updated from pg_stat_database;
 datid |  datname  | tup_inserted | tup_updated
-------+-----------+--------------+-------------
     0 |           |           24 |           5
     5 | postgres  |            0 |           0
     1 | template1 |        17520 |         743
     4 | template0 |            0 |           0
(4 rows)
```

To demonstrate a hypothetical postgres crash or restart, we can refresh the postgresstatistics with `pg_stat_reset()`.

The sample agent does NOT handle true postgres crashes or restarts, where a connection may fail.



## setup and test


Start postgres db:

```
podman run --name pg-test --rm -e POSTGRES_PASSWORD='123' -d -p 5432:5432 postgres
```

export the database URL and connection parameters for pgbench:

```
export DATABASE_URL='postgres://postgres:123@localhost:5432'

export PGHOST='localhost'
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=123
```

start your metrics agent

```
go run ./main.go >& /tmp/agent.log &
```

start pg_bench. In this example, the metrics we're expecting to increment just the postgres DB, and leave the others untouched, as our pgdatabase will be the postgres one

```
pgbench -i -s100
```


curl your metrics, to see that the total counter increments permanently up, while the guage only updates according to the polling interval:

```
pg_agent(main*)$ curl -s http://localhost:5480/metrics | grep pg
```

sample output:
```
# HELP pg_tup_inserted Guage, number of rows inserted by queries in this database
# TYPE pg_tup_inserted gauge
pg_tup_inserted{datid="1",datname="template1"} 17520
pg_tup_inserted{datid="4",datname="template0"} 0
pg_tup_inserted{datid="5",datname="postgres"} 5.100759e+06    <<<< this row is a guage, it does not continually increment
# HELP pg_tup_inserted_total Number of rows inserted by queries in this database
# TYPE pg_tup_inserted_total counter
pg_tup_inserted_total{datid="1",datname="template1"} 17520
pg_tup_inserted_total{datid="4",datname="template0"} 0
pg_tup_inserted_total{datid="5",datname="postgres"} 5.100759e+06 << this row is a counter, it will continually increment
# HELP pg_tup_updated Guage, number of rows updated by queries in this database
# TYPE pg_tup_updated gauge
pg_tup_updated{datid="1",datname="template1"} 743
pg_tup_updated{datid="4",datname="template0"} 0
pg_tup_updated{datid="5",datname="postgres"} 43
# HELP pg_tup_updated_total Number of rows updated by queries in this database
# TYPE pg_tup_updated_total counter
pg_tup_updated_total{datid="1",datname="template1"} 743
pg_tup_updated_total{datid="4",datname="template0"} 0
pg_tup_updated_total{datid="5",datname="postgres"} 43

```

Observe that running `pg_stat_reset()` does drop the values from pg_side, but does not reset our counter in the agent

```
psql -c "select pg_stat_reset()"
psql -c "select select datid, datname, tup_inserted, tup_updated from pg_stat_database;"

agent(main*)$ psql -c "select datid,datname,tup_inserted,tup_updated from pg_stat_database;"
 datid |  datname  | tup_inserted | tup_updated 
-------+-----------+--------------+-------------
     0 |           |           24 |           5
     5 | postgres  |            0 |           0
     1 | template1 |        17520 |         743
     4 | template0 |            0 |           0
(4 rows)

pg_agent(main*)$ curl -s http://localhost:5480/metrics | grep inserted_total
# HELP pg_tup_inserted_total Number of rows inserted by queries in this database
# TYPE pg_tup_inserted_total counter
pg_tup_inserted_total{datid="1",datname="template1"} 17520
pg_tup_inserted_total{datid="4",datname="template0"} 0
pg_tup_inserted_total{datid="5",datname="postgres"} 1.0101403e+07

```

## Foward looking things....


1) in prometheus, set up a scrape interval that's the same as the local scrape interval to avoid weird interleaving. Or, even better, write a trigger on the HTTP handler to trigger a DB metrics scrape based on when it gets polled by prometheus itself to avoid the issue entirely

2) use RATE to look at ops per window in prometheus

3) metrics are exported with lables for database name and datid - which is great for filtering and reporting against different 

4) there's many, many more metrics in `pg_stat_database`
