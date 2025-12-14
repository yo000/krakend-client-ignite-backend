package main

/*
 * krakend.io backend plugin to Apache Ignite 2.x
 */

// curl -i -XPOST http://localhost:8080/v1/ignite-tcp -d '{ "schema": "PUBLIC", "query": "SELECT * FROM PUBLIC.ORGANIZATION", "gettypes": true}'

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xwb1989/sqlparser"
	_ "github.com/yo000/ignite-go-client/sql"
)

const (
	pluginName = "krakend-client-ignite-backend"
)

var (
	gMaxIdleConn     = 3
	gMaxOpenConn     = 10
	gConnMaxLifetime = 0
	// Query timeout in milliseconds
	gMsTimeout       = 60000
)

// ClientRegisterer is the symbol the plugin loader will try to load. It must implement the RegisterClient interface
var ClientRegisterer = registerer(pluginName)

type registerer string

type ClientQuery struct {
	Schema string `json:"schema"`
	Query  string `json:"query"`
	GetTypes bool `json:"gettypes"`
}

var logger Logger = nil

type Result struct {
	Success        bool                     `json:"success"`
	Message        string                   `json:"message"`
	Count          int                      `json:"count"`
	QueryTimestamp string                   `json:"querytimestamp"`
	Response       []map[string]interface{} `json:"data"`
}

type ResultWithType struct {
	Result
	DataTypes      map[string]string        `json:"datatypes"`
}

func (registerer) RegisterLogger(v interface{}) {
	l, ok := v.(Logger)
	if !ok {
		return
	}
	logger = l
	logger.Debug(fmt.Sprintf("[PLUGIN: %s] Logger loaded", ClientRegisterer))
}

func (r registerer) RegisterClients(f func(
	name string,
	handler func(context.Context, map[string]interface{}) (http.Handler, error),
)) {
	f(string(r), r.registerClients)
}

func getMaxIdleConn(config map[string]interface{}) int {
	m, ok := config["max-idle-conn"]
	if !ok {
		return gMaxIdleConn
	}
	switch m.(type) {
		case float64:
			return int(m.(float64))
		case int:
		case int64:
		default:
			logger.Error(fmt.Sprintf("max-idle-conn is an unknown type: %T; setting default", m))
	}
	return gMaxIdleConn
}

func getMaxOpenConn(config map[string]interface{}) int {
	m, ok := config["max-open-conn"]
	if !ok {
		return gMaxOpenConn
	}
	switch m.(type) {
		case float64:
			return int(m.(float64))
		case int:
		case int64:
		default:
			logger.Error(fmt.Sprintf("max-open-conn is an unknown type: %T; setting default", m))
	}
	return gMaxOpenConn
}

func getMaxConnLifetime(config map[string]interface{}) int {
	m, ok := config["conn-max-lifetime"]
	if !ok {
		return gConnMaxLifetime
	}
	switch m.(type) {
		case float64:
			return int(m.(float64))
		case int:
		case int64:
		default:
			logger.Error(fmt.Sprintf("conn-max-lifetime is an unknown type: %T; setting default", m))
	}
	return gConnMaxLifetime
}

func getTimeout(config map[string]interface{}) int {
	m, ok := config["timeout"]
	if !ok {
		return gMsTimeout
	}
	switch m.(type) {
		case float64:
			return int(m.(float64))
		case int:
		case int64:
		default:
			logger.Error(fmt.Sprintf("timeout is an unknown type: %T; setting default", m))
	}
	return gMsTimeout
}

func (r registerer) registerClients(_ context.Context, extra map[string]interface{}) (http.Handler, error) {
	// check the passed configuration and initialize the plugin
	name, ok := extra["name"].(string)
	if !ok {
		return nil, errors.New("wrong config")
	}
	if name != string(r) {
		return nil, fmt.Errorf("unknown register %s", name)
	}

	// check the cfg. If the modifier requires some configuration,
	// it should be under the name of the plugin. E.g.:
	/*
	"extra_config":{
		"plugin/http-client":{
			"name":"krakend-client-ignite-backend",
			"krakend-client-ignite-backend":{
				"server": "ignite.mine.lan",
				"port": 10800,
				"username": "krakend",
				"password": "password",
				"@comment": "Queries are not limited to this table, but it should exist. timeout is in milliseconds.",
				"table": "SQL_PUBLIC_ORGANIZATION",
				"tls": "no",
				"tls-insecure": "no",
				"max-idle-conn": 3,
				"max-open-conn": 10,
				"conn-max-lifetime": 0,
				"timeout": 60000
			}
		}
	}
	*/

	// The config variable contains all the keys you have defined in the configuration:
	config, _ := extra[pluginName].(map[string]interface{})

	logger.Info(fmt.Sprintf("%s: Initializing connection to ignite", pluginName))
	// Initialize ignite connection pool
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := openDatabase(config, ctx)
	if err != nil {
		logger.Error(fmt.Sprintf("%s: Error initializing ignite connection : %s", pluginName, err.Error()))
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { 
			http.Error(w, "Backend is not ready", http.StatusInternalServerError)
			return
		}), err
	}
	// FIXME: Where to do this ? 
	//defer db.Close()

	pool.SetConnMaxLifetime(time.Duration(getMaxConnLifetime(config)))
	pool.SetMaxIdleConns(getMaxIdleConn(config))
	pool.SetMaxOpenConns(getMaxOpenConn(config))
	logger.Info(fmt.Sprintf("%s: Ignite MaxConnLifetime set to %ds", pluginName, getMaxConnLifetime(config)))
	logger.Info(fmt.Sprintf("%s: Ignite MaxIdleConn set to %d", pluginName, getMaxIdleConn(config)))
	logger.Info(fmt.Sprintf("%s: Ignite MaxOpenConn set to %d", pluginName, getMaxOpenConn(config)))
	logger.Info(fmt.Sprintf("%s: Ignite query timeout set to %dms", pluginName, gMsTimeout))
	logger.Info(fmt.Sprintf("%s: Ignite connection initialized successfully", pluginName))

	// return the actual handler wrapping or your custom logic so it can be used as a replacement for the default http handler
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Timestamp is a way to trace this query
		ts := time.Now()
		tss := ts.Format(time.RFC3339Nano)
		if strings.EqualFold(req.Method, "POST") {
			decoder := json.NewDecoder(req.Body)

			var q ClientQuery
			err := decoder.Decode(&q)
			if err != nil {
				propagateError(w, "", err, tss)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Should I ?
			req.Body.Close()

			//logger.Debug("Post OK, body was read: ", fmt.Sprintf("%v", q))
			decodedQuery := strings.ReplaceAll(q.Query, "%27", "'")

			// Validate query
			if err := validateQuery(decodedQuery); err != nil {
				propagateError(w, decodedQuery, err, tss)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := SelectQuerySqlWithType(pool, ctx, decodedQuery, tss)
			if err != nil {
				propagateError(w, decodedQuery, err, tss)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Add("Content-Type", "application/json")
			var b []byte
			if q.GetTypes {
				b, _ = json.Marshal(res)
			} else {
				b, _ = json.Marshal(res.Result)
			}
			w.Write(b)
			logger.Info(fmt.Sprintf("%s: request: \"%s\" at %s", pluginName, decodedQuery, tss))
			return
		} else {
			// We should never land here as krakend have a "method" property which shoud be set on POST
			http.Error(w, "Only POST method is supported", http.StatusInternalServerError)
			return
		}
	}), nil
}

// Send error to client
func propagateError(w http.ResponseWriter, query string, err error, ts string) {
	finalRes := Result{Success: false, Message: err.Error(),
		Count: 0, Response: nil, QueryTimestamp: ts}
	w.Header().Add("Content-Type", "application/json")
	b, _ := json.Marshal(finalRes)
	w.Write(b)
	logger.Error(fmt.Sprintf("%s: Request: \"%s\" at %s: %s", pluginName, query, ts, err.Error()))
}

func checkIgniteArgs(config map[string]interface{}) error {
	if _, ok := config["server"]; ok != true {
		return fmt.Errorf("server not found in %s config", pluginName)
	}
	p, ok := config["port"]
	if !ok {
		return fmt.Errorf("port not found in %s config", pluginName)
	}
	switch p.(type) {
		case float64:
			po := int(p.(float64))
			delete(config, "port")
			config["port"] = po
		case int:
		case int64:
		default:
			return fmt.Errorf("port is an unknown type: %T", p)
	}
	if _, ok := config["username"]; ok != true {
		return fmt.Errorf("username not found in %s config", pluginName)
	}
	if _, ok := config["password"]; ok != true {
		return fmt.Errorf("password not found in %s config", pluginName)
	}
	if _, ok := config["table"]; ok != true {
		return fmt.Errorf("table not found in %s config", pluginName)
	}
	t, ok := config["tls"]
	if ok != true {
		return fmt.Errorf("tls not found in %s config", pluginName)
	}
	if !strings.EqualFold(t.(string), "yes") && !strings.EqualFold(t.(string), "no") {
		return fmt.Errorf("tls should be \"yes\" or \"no\"")
	}
	ti, ok := config["tls-insecure"]
	if ok != true {
		return fmt.Errorf("tls-insecure not found in %s config", pluginName)
	}
	if !strings.EqualFold(ti.(string), "yes") && !strings.EqualFold(ti.(string), "no") {
		return fmt.Errorf("tls-insecure should be \"yes\" or \"no\"")
	}

	return nil
}

func removeSqlParserPrefix(errorMsg string) string {
	return strings.Replace(errorMsg, "*sqlparser.", "", 1)
}

// Test if query is valid, and a supported type (only SELECT currently)
func validateQuery(query string) error {
	stmt, err := sqlparser.Parse(query)
	if err != nil {
		return fmt.Errorf("Invalid query : %v\n", err)
	}

	switch stmt.(type) {
		case *sqlparser.Select:
			return nil
		case *sqlparser.Insert, *sqlparser.Update, *sqlparser.Delete, *sqlparser.DDL:
			return fmt.Errorf(removeSqlParserPrefix(fmt.Sprintf("Unsupported SQL statement : %T", stmt)))
		default:
			return fmt.Errorf(removeSqlParserPrefix(fmt.Sprintf("Unknown SQL statement : %T", stmt)))
	}
	return fmt.Errorf("Unknown error in validateQuery")
}

func openDatabase(config map[string]interface{}, ctx context.Context) (*sql.DB, error) {
	if err := checkIgniteArgs(config); err != nil {
		return nil, err
	}
	gMsTimeout  = getTimeout(config)
	// Does not seem to affect queries
	pagesize := 10000
	url := fmt.Sprintf("tcp://%s:%d/%s?version=1.1.0&username=%s&password=%s&tls=%s&tls-insecure-skip-verify=%s&page-size=%d&timeout=%d", config["server"], config["port"], config["table"], config["username"], config["password"], config["tls"], config["tls-insecure"], pagesize, gMsTimeout)
	// Danger : password will be in logs
	//logger.Debug(fmt.Sprintf("%s: url = %s", pluginName, url))
	db, err := sql.Open("ignite", url)
	if err != nil {
		return db, fmt.Errorf("failed to open connection: %v", err)
	}
	
	// ping
	if err = db.PingContext(ctx); err != nil {
		return db, fmt.Errorf("ping failed: %v", err)
	}
	return db, nil
}

func SelectQuerySqlWithType(db *sql.DB, ctx context.Context, query string, ts string) (ResultWithType, error) {
	var finalRes ResultWithType

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return finalRes, fmt.Errorf("failed sql query %s: %v", query, err)
	}

	columns, _ := rows.Columns()
	colCount := len(columns)
	values := make([]interface{}, colCount)
	// Pointer array to give to Rows.Scan, values will be saved into values array
	valuePtrs := make([]interface{}, colCount)
	var results []map[string]interface{}
	finalRes.DataTypes = make(map[string]string)

	for rows.Next() {
		// Get column types only once a row was parsed
		if len(results) == 1 {
			ct, _ := rows.ColumnTypes()
			for _, c := range ct {
				finalRes.DataTypes[c.Name()] = c.DatabaseTypeName()
			}
		}

		result := make(map[string]interface{})
		// initialize our pointers
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return finalRes, fmt.Errorf("failed to get row: %v", err)
		}

		for i, col := range columns {
			val := values[i]
			b, ok := val.([]byte)
			var v interface{}
			if ok {
				v = string(b)
			} else {
				v = val
			}
			result[col] = v
		}
		results = append(results, result)
	}
	rows.Close()

	finalRes.Success  = true
	finalRes.Count    = len(results)
	finalRes.Message  = ""
	finalRes.Response = results
	finalRes.QueryTimestamp = ts

	return finalRes, nil
}


func main() {}

type Logger interface {
	Debug(v ...interface{})
	Info(v ...interface{})
	Warning(v ...interface{})
	Error(v ...interface{})
	Critical(v ...interface{})
	Fatal(v ...interface{})
}



