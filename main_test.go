package main

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

func TestHandlePostUpload(t *testing.T) {
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

			s.handlePostUploadFile(w, req)

			require.Equal(t, test.wantStatus, w.Result().StatusCode)
		})
	}
}

type mockObjStore struct{
	err error
}

func (m mockObjStore) MakeBucket(_ context.Context, _ string, _ minio.MakeBucketOptions) error {
	panic("not implemented")
}

func (m mockObjStore) BucketExists(_ context.Context, _ string) (bool, error) {
	panic("not implemented")
}

func (m mockObjStore)	PutObject(_ context.Context, _, _ string, _ io.Reader, size int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	if m.err != nil {
		return minio.UploadInfo{}, m.err
	}

	return minio.UploadInfo{Size: size}, nil
}
