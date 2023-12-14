package main

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

func TestHandlePostUploadFile(t *testing.T) {
	tests := []struct{
		name string
		wantStatus int
	}{
		{
			name: "should work",
			wantStatus: http.StatusCreated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mockObjStore{err: nil}
			s := NewServer(store, "testBucket")

			pr, pw := io.Pipe()
			writer := multipart.NewWriter(pw)

			go func () {
				defer writer.Close()

				ff, err := writer.CreateFormFile("file", "testFileName.text")
				require.NoError(t, err)

				_, err = ff.Write([]byte("test file contents"))
				require.NoError(t, err)
			}()

			req := httptest.NewRequest(http.MethodPost, "/upload", pr)
			req.Header.Add("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			s.handlePostUploadFile(w, req, nil)

			require.Equal(t, test.wantStatus, w.Result().StatusCode)
		})
	}
}

func TestHandleGetFile(t *testing.T) {
	tests := []struct{
		name string
		objectBody string
		readerError error
		wantStatus int
	}{
		{
			name: "should work",
			objectBody: "test file contents",
			wantStatus: http.StatusOK,
		},
		{
			name: "file not found",
			readerError: errors.New("The specified key does not exist."),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mockObjStore{
				objectBody:  test.objectBody,
				readerError: test.readerError,
				err:         nil,
			}
			s := NewServer(store, "testBucket")

			req := httptest.NewRequest(http.MethodGet, "/file/filename", nil)
			w := httptest.NewRecorder()

			s.handleGetFile(w, req, nil)

			require.Equal(t, test.wantStatus, w.Result().StatusCode)
		})
	}
}

type mockObjStore struct {
	objectBody string
	readerError error
	err error
}

func (m mockObjStore) PutObject(_ context.Context, _, _ string, _ io.Reader, size int64) (minio.UploadInfo, error) {
	if m.err != nil {
		return minio.UploadInfo{}, m.err
	}

	return minio.UploadInfo{Size: size}, nil
}

func (m mockObjStore) GetObject(_ context.Context, _, _ string) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}

	if m.readerError != nil {
		return io.NopCloser(errorReader{err: m.readerError}), nil
	}

	obj := strings.NewReader(m.objectBody)
	return io.NopCloser(obj), nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
