package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"path"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/sio"
	"golang.org/x/crypto/argon2"
)

// Since this is a demo projct, I have included the configuration here, but for
// a production system these would be stored externally
const (
	minioEndpoint   = "127.0.0.1:9000"
	accessKeyID     = "minioadmin"
	secretAccessKey = "minioadmin"
	bucketName      = "filesrv"
	encryptionKey   = "a static encryption key"

	// minio can handle uploading in parts for us, but it doesn't exactly match
	// the given spec because the minimum chunk size is 5MB
	chunkSize = 10 << 19 // ~ 5MB
)

// objStorer abstracts the minio operations to allow dependency injection
type objStorer interface {
	PutObject(ctx context.Context, bucketName, filename string, file io.Reader, size, chunkSize int64) (minio.UploadInfo, error)
	GetObject(ctx context.Context, bucketName, filename string) (io.ReadCloser, error)
}

// minioStore wraps the needed minio functions to allow for easier testing
type minioStore struct {
	c *minio.Client
}

func (m minioStore) PutObject(ctx context.Context, bucketName, filename string, f io.Reader, size, chunkSize int64) (minio.UploadInfo, error) {
	return m.c.PutObject(ctx, bucketName, filename, f, size, minio.PutObjectOptions{PartSize: uint64(chunkSize)})
}

func (m minioStore) GetObject(ctx context.Context, bucketName, filename string) (io.ReadCloser, error) {
	return m.c.GetObject(ctx, bucketName, filename, minio.GetObjectOptions{})
}

// server stores the dependencies for the http handlers
type server struct {
	minioClient   objStorer
	bucketName    string
	encryptionKey string
	chunkSize     int64
}

func NewServer(minioClient objStorer, bucketName, encryptionKey string, chunkSize int64) server {
	return server{
		minioClient:   minioClient,
		bucketName:    bucketName,
		encryptionKey: encryptionKey,
		chunkSize:     chunkSize,
	}
}

// handlePostUploadFile accepts a file in the form with key "file", encrypts the
// contents and stores it in minio
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

	// I chose to use the encryption method detailed in the minio documentation,
	// since it is designed for data at rest, works well with minio, and is
	// relativly well used.
	salt := []byte(path.Join(s.bucketName, handler.Filename))
	encrypted, err := sio.EncryptReader(file, sio.Config{
		Key: argon2.IDKey([]byte(s.encryptionKey), salt, 1, 64*1024, 4, 32),
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("encrypt file:", err)
		return
	}

	encryptedSize, err := sio.EncryptedSize(uint64(handler.Size))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("encrypted size:", err)
		return
	}

	info, err := s.minioClient.PutObject(r.Context(), s.bucketName, handler.Filename, encrypted, int64(encryptedSize), s.chunkSize)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("put object: filename: %s, error: %s", handler.Filename, err)
		return
	}

	log.Println("uploaded file", handler.Filename, "of size", info.Size)

	// I am just using status codes for responses here because it is a demo
	// project, a real service would include a response body with more
	// information such as more detailed errors.
	w.WriteHeader(http.StatusCreated)
}

// handleGetFile gets the file with name given in the URL, decrypts it and
// returns it in the response body
func (s server) handleGetFile(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	filename := ps.ByName("filename")
	obj, err := s.minioClient.GetObject(r.Context(), s.bucketName, filename)
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

	salt := []byte(path.Join(s.bucketName, filename))
	_, err = sio.Decrypt(w, obj, sio.Config{
		Key: argon2.IDKey([]byte(s.encryptionKey), salt, 1, 64*1024, 4, 32),
	})
	if err != nil {
		if err.Error() == "The specified key does not exist." {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		log.Println("decrypt file:", err)
		return
	}
}

func main() {
	// Initialize minio client object.
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
	})
	if err != nil {
		log.Fatalln(err)
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Minute)
	defer cancelFunc()

	err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	if err != nil {
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, errBucketExists := minioClient.BucketExists(ctx, bucketName)
		if errBucketExists == nil && exists {
			log.Printf("We already own %s\n", bucketName)
		} else {
			log.Fatalln(err)
		}
	} else {
		log.Printf("Successfully created bucket %s\n", bucketName)
	}

	s := NewServer(minioStore{c: minioClient}, bucketName, encryptionKey, chunkSize)

	// I used the httprouter package because it allows me to easily expose the
	// API that I want with minimal code.
	router := httprouter.New()
	router.POST("/upload", s.handlePostUploadFile)
	router.GET("/file/:filename", s.handleGetFile)

	err = http.ListenAndServe(":2001", router)
	if err != nil {
		log.Fatalln(err)
	}
}
