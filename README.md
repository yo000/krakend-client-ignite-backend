# krakend-client-ignite-backend
A krakend.io backend plugin to Apache Ignite 2.x.  
Initiates a pool of connections to Ignite over TCP binary protocol. It could replace Ignite REST module with better performances for read-only.  
Currently support sql SELECT operations.  

## How to use
krakend.json:
```
{
  "$schema": "https://www.krakend.io/schema/v2.5/krakend.json",
  "version": 3,
  "port": 8080,
  "plugin": {
    "pattern": ".so",
    "folder": "/usr/local/krakend/plugins/"
  },
  "endpoints": [
    {
      "endpoint": "/v1/ignite-tcp",
      "method": "POST",
      "backend": [
        {
          "host": [
            "http://127.0.0.1:8000"
          ],
          "url_pattern": "/",
          "extra_config": {
            "plugin/http-client": {
              "name":"krakend-client-ignite-backend",
              "krakend-client-ignite-backend":{
                "server": "127.0.0.1",
                "port": 10800,
                "username": "ignite",
                "password": "ignite",
                "@comment": "Queries are not limited to this table, but it should exist. timeout is in milliseconds.",
                "table": "SQL_PUBLIC_ORGANIZATION",
                "tls": "no",
                "tls-insecure": "no",
                "max-idle-conn": 3,
                "max-open-conn": 10,
                "conn-max-lifetime": 0,
                "timeout": 30000
              }
            }
          }
        }
      ]
    }
  ]
}
```

Query ignite through krakend with curl:
```
curl -i -XPOST http://localhost:8080/v1/ignite-tcp -d '{ "schema": "PUBLIC", "query": "SELECT * FROM PUBLIC.ORGANIZATION"}'
{
  "count": 4,
  "data": [
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.099531492Z",
      "KEY": 11,
      "NAME": "Org 11"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704077Z",
      "KEY": 12,
      "NAME": "Org 12"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704127Z",
      "KEY": 13,
      "NAME": "Org 13"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704177Z",
      "KEY": 14,
      "NAME": "Org 14"
    }
  ],
  "message": "",
  "querytimestamp": "2025-12-14T17:47:21.244104422+01:00",
  "success": true
}
```

Add data types to output with gettypes parameter:
```
curl -i -XPOST http://localhost:8080/v1/ignite-tcp -d '{ "schema": "PUBLIC", "query": "SELECT * FROM PUBLIC.ORGANIZATION", "gettypes": true}'
{
  "count": 4,
  "data": [
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.099531492Z",
      "KEY": 11,
      "NAME": "Org 11"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704077Z",
      "KEY": 12,
      "NAME": "Org 12"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704127Z",
      "KEY": 13,
      "NAME": "Org 13"
    },
    {
      "FOUNDDATETIME": "2025-12-12T19:18:53.066704177Z",
      "KEY": 14,
      "NAME": "Org 14"
    }
  ],
  "datatypes": {
    "FOUNDDATETIME": "TIMESTAMP",
    "KEY": "INT",
    "NAME": "VARCHAR"
  },
  "message": "",
  "querytimestamp": "2025-12-14T17:46:45.396464789+01:00",
  "success": true
}
```

## How to build
See https://www.krakend.io/docs/extending/writing-plugins/

