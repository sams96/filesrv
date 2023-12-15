Start with
```
$ docker-compose up -d
$ go run main.go
```

To upload a file:
```
$ curl 127.0.0.1:2001/upload -F file=@filename
```

To get a file:
```
$ curl 127.0.0.1:2001/file/filename
```
