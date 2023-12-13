package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	minioEndpoint = "127.0.0.1:9000"
	accessKeyID = "minioadmin"
	secretAccessKey = "minioadmin"
)	

type objStore interface {
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	BucketExists(context.Context, string) (bool, error)	
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

type server struct {
	minioClient objStore
	bucketName string
}

func NewServer (minioClient objStore, bucketName string) server {
	return server{
		minioClient: minioClient,
		bucketName: bucketName,
	}
}

func (s server) handlePostUploadFile(w http.ResponseWriter, r *http.Request) {
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

	info, err := s.minioClient.PutObject(r.Context(), s.bucketName, handler.Filename, file, handler.Size, minio.PutObjectOptions{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("put object:", err)
	}

	log.Println("uploaded file", handler.Filename, "of size", info.Size)

	w.WriteHeader(http.StatusCreated)
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

	s := NewServer(minioClient, bucketName)

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", s.handlePostUploadFile)

	err = http.ListenAndServe(":2001", mux)
	if err != nil {
		log.Fatalln(err)
	}
}
