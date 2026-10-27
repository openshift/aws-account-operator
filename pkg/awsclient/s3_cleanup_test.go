package awsclient

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestBatchDeleteBucketObjectsResumesAfterCancellation(t *testing.T) {
	keys := make(map[string]bool, 1501)
	for i := range 1501 {
		keys[fmt.Sprintf("object-%04d", i)] = true
	}
	listCalls, deleteCalls := 0, 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			listCalls++
			_, _ = fmt.Fprint(w, "<ListBucketResult><IsTruncated>false</IsTruncated>")
			count := 0
			for i := range 1501 {
				key := fmt.Sprintf("object-%04d", i)
				if keys[key] && count < 1000 {
					_, _ = fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", key)
					count++
				}
			}
			_, _ = fmt.Fprint(w, "</ListBucketResult>")
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			deleteCalls++
			var request struct {
				Objects []struct {
					Key string `xml:"Key"`
				} `xml:"Object"`
			}
			if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode DeleteObjects: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(request.Objects) > 1000 {
				t.Errorf("DeleteObjects sent %d keys, max 1000", len(request.Objects))
			}
			for _, object := range request.Objects {
				delete(keys, object.Key)
			}
			_, _ = fmt.Fprint(w, "<DeleteResult/>")
			if deleteCalls == 1 {
				cancel()
			}
		default:
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := &awsClient{s3Client: s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL), HTTPClient: server.Client(),
	}, func(o *s3.Options) { o.UsePathStyle = true })}

	if err := client.BatchDeleteBucketObjects(ctx, aws.String("test-bucket")); err == nil {
		t.Fatal("expected the canceled attempt to stop")
	}
	if len(keys) != 501 {
		t.Fatalf("first attempt left %d keys, want 501", len(keys))
	}
	if err := client.BatchDeleteBucketObjects(context.Background(), aws.String("test-bucket")); err != nil {
		t.Fatalf("resumed deletion failed: %v", err)
	}
	if len(keys) != 0 || deleteCalls != 2 || listCalls < 3 {
		t.Fatalf("incomplete resumed deletion: remaining=%d deletes=%d lists=%d", len(keys), deleteCalls, listCalls)
	}
}

func TestBatchDeleteBucketObjectsReportsPartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, "<ListBucketResult><Contents><Key>protected</Key></Contents></ListBucketResult>")
			return
		}
		_, _ = fmt.Fprint(w, "<DeleteResult><Error><Key>protected</Key><Code>AccessDenied</Code><Message>denied</Message></Error></DeleteResult>")
	}))
	defer server.Close()
	client := &awsClient{s3Client: s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		BaseEndpoint: aws.String(server.URL), HTTPClient: server.Client(),
	}, func(o *s3.Options) { o.UsePathStyle = true })}

	err := client.BatchDeleteBucketObjects(context.Background(), aws.String("test-bucket"))
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("expected per-object AccessDenied, got %v", err)
	}
}
