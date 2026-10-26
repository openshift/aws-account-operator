package awsclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestS3ClientForBucketUsesBucketRegion(t *testing.T) {
	const bucket = "cross-region-bucket"
	var requests []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.Host)
		response := &http.Response{Header: make(http.Header), Request: request}
		switch request.Method {
		case http.MethodHead:
			response.StatusCode = http.StatusMovedPermanently
			response.Header.Set("x-amz-bucket-region", "us-west-2")
			response.Body = io.NopCloser(strings.NewReader(""))
		case http.MethodGet:
			if request.URL.Host != bucket+".s3.us-west-2.amazonaws.com" {
				t.Errorf("object listing used wrong endpoint: %s", request.URL.Host)
			}
			response.StatusCode = http.StatusOK
			response.Header.Set("Content-Type", "application/xml")
			response.Body = io.NopCloser(strings.NewReader("<ListBucketResult><Name>" + bucket + "</Name><Contents><Key>object</Key></Contents></ListBucketResult>"))
		default:
			t.Errorf("unexpected S3 request: %s", request.Method)
			response.StatusCode = http.StatusInternalServerError
			response.Body = io.NopCloser(strings.NewReader(""))
		}
		return response, nil
	})
	client := &awsClient{s3Client: s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
		HTTPClient:  &http.Client{Transport: transport},
	})}

	regional, err := client.S3ClientForBucket(context.Background(), bucket)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := regional.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects.Contents) != 1 || aws.ToString(objects.Contents[0].Key) != "object" {
		t.Fatalf("unexpected objects: %+v", objects.Contents)
	}
	if len(requests) != 2 || requests[0] != "HEAD "+bucket+".s3.us-east-1.amazonaws.com" || requests[1] != "GET "+bucket+".s3.us-west-2.amazonaws.com" {
		t.Fatalf("unexpected endpoints: %v", requests)
	}
}

func TestS3ClientForBucketUsesRegionFrom404(t *testing.T) {
	const bucket = "cross-region-bucket"
	var requests []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.Host)
		response := &http.Response{Header: make(http.Header), Request: request}
		switch request.Method {
		case http.MethodHead:
			response.StatusCode = http.StatusNotFound
			response.Header.Set("x-amz-bucket-region", "us-west-2")
			response.Body = io.NopCloser(strings.NewReader(""))
		case http.MethodGet:
			response.StatusCode = http.StatusOK
			response.Header.Set("Content-Type", "application/xml")
			response.Body = io.NopCloser(strings.NewReader("<ListBucketResult><Contents><Key>object</Key></Contents></ListBucketResult>"))
		default:
			t.Errorf("unexpected S3 request: %s", request.Method)
			response.StatusCode = http.StatusInternalServerError
			response.Body = io.NopCloser(strings.NewReader(""))
		}
		return response, nil
	})
	client := &awsClient{s3Client: s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
		HTTPClient:  &http.Client{Transport: transport},
	})}

	regional, err := client.S3ClientForBucket(context.Background(), bucket)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := regional.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects.Contents) != 1 || aws.ToString(objects.Contents[0].Key) != "object" {
		t.Fatalf("unexpected objects: %+v", objects.Contents)
	}
	if len(requests) != 2 || requests[0] != "HEAD "+bucket+".s3.us-east-1.amazonaws.com" || requests[1] != "GET "+bucket+".s3.us-west-2.amazonaws.com" {
		t.Fatalf("unexpected endpoints: %v", requests)
	}
}
