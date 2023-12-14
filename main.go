package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	minioEndpoint = "127.0.0.1:9000"
	accessKeyID = "minioadmin"
	secretAccessKey = "minioadmin"
)	

type objStorer interface {
	PutObject(context.Context, string, string, io.Reader, int64) (minio.UploadInfo, error)
	GetObject(context.Context, string, string) (io.ReadCloser, error)
}

type minioStore struct {
	c *minio.Client
}

func (m minioStore) PutObject(ctx context.Context, bucketName, filename string, f io.Reader, size int64) (minio.UploadInfo, error) {
	return m.c.PutObject(ctx, bucketName, filename, f, size, minio.PutObjectOptions{})
}

func (m minioStore) GetObject(ctx context.Context, bucketName, filename string) (io.ReadCloser, error) {
	return m.c.GetObject(ctx, bucketName, filename, minio.GetObjectOptions{})
}

type server struct {
	minioClient objStorer
	bucketName string
}

func NewServer (minioClient objStorer, bucketName string) server {
	return server{
		minioClient: minioClient,
		bucketName: bucketName,
	}
}

func (s server) handlePostUploadFile(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("parse form:", err)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("form file:", err)
		return
	}
	defer file.Close()

	info, err := s.minioClient.PutObject(r.Context(), s.bucketName, handler.Filename, file, handler.Size)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("put object filename %s:, error %s", handler.Filename, err)
	}

	log.Println("uploaded file", handler.Filename, "of size", info.Size)

	w.WriteHeader(http.StatusCreated)
}

func (s server) handleGetFile(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	obj, err := s.minioClient.GetObject(r.Context(), s.bucketName, ps.ByName("filename"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("get object:", err)
		return
	}
	if obj == nil {
		w.WriteHeader(http.StatusNotFound)
		log.Println("file not found")
		return
	}
	defer obj.Close()

	body, err := io.ReadAll(obj)
	if err != nil {
		if err.Error() == "The specified key does not exist." {
			w.WriteHeader(http.StatusNotFound)
			log.Println("file not found")
			return
		}
		
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("read file:", err)
		return
	}
	if len(body) == 0 {
		w.WriteHeader(http.StatusNotFound)
		log.Println("file not found")
		return
	}

	_, err = w.Write(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("write body:", err)
		return
	}
}

func main() {
	// Initialize minio client object.
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
	})
	if err != nil {
		log.Fatalln(err)
	}

	// Make a new bucket called testbucket.
	bucketName := "testbucket"
	location := "eu-west-1"

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Minute)
	defer cancelFunc()

	err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: location})
	if err != nil {
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, errBucketExists := minioClient.BucketExists(ctx, bucketName)
		if errBucketExists == nil && exists {
			log.Printf("We already own %s\n", bucketName)
		} else {
			log.Fatalln(err)
		}
	} else {
		log.Printf("Successfully created %s\n", bucketName)
	}

	s := NewServer(minioStore{c: minioClient}, bucketName)

	router := httprouter.New()
	router.POST("/upload", s.handlePostUploadFile)
	router.GET("/file/:filename", s.handleGetFile)

	err = http.ListenAndServe(":2001", router)
	if err != nil {
		log.Fatalln(err)
	}
}
