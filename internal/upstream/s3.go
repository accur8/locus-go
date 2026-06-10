package upstream

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Object is one object in a listing.
type S3Object struct {
	Key                string
	Size               int64
	LastModifiedMillis int64 // epoch-seconds precision, *1000 (mirrors Scala)
}

// S3Entry is either a common-prefix "directory" or an object (S3Assist Either).
type S3Entry struct {
	IsDir   bool
	DirName string
	Obj     S3Object
}

// S3Client wraps the AWS SDK v2 S3 client.
type S3Client struct {
	cl *s3.Client
}

// NewS3Client builds an S3 client with static credentials for the given region.
func NewS3Client(accessKey, secretKey, region string) *S3Client {
	cl := s3.NewFromConfig(aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	})
	return &S3Client{cl: cl}
}

// List mirrors S3Assist.list2: ListObjectsV2 with delimiter "/" under prefixDir
// (a directory path ending in "/"). Common prefixes become directory entries
// (with the prefix and trailing slash stripped); contents become objects. Order
// is, per page, common-prefixes then contents.
func (c *S3Client) List(ctx context.Context, bucket, prefixDir string) ([]S3Entry, error) {
	pager := s3.NewListObjectsV2Paginator(c.cl, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefixDir),
		Delimiter: aws.String("/"),
	})
	var out []S3Entry
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, cp := range page.CommonPrefixes {
			p := aws.ToString(cp.Prefix)
			name := p
			if strings.HasPrefix(p, prefixDir) && strings.HasSuffix(p, "/") {
				name = p[len(prefixDir) : len(p)-1]
			}
			out = append(out, S3Entry{IsDir: true, DirName: name})
		}
		for _, o := range page.Contents {
			var ms int64
			if o.LastModified != nil {
				ms = o.LastModified.Unix() * 1000
			}
			out = append(out, S3Entry{Obj: S3Object{
				Key:                aws.ToString(o.Key),
				Size:               aws.ToInt64(o.Size),
				LastModifiedMillis: ms,
			}})
		}
	}
	return out, nil
}

// GetObject downloads an object to destFile. found=false on a 404/NoSuchKey.
func (c *S3Client) GetObject(ctx context.Context, bucket, key, destFile string) (found bool, err error) {
	resp, err := c.cl.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	defer resp.Body.Close()
	f, err := os.Create(destFile)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	return true, nil
}

// HeadObject reports whether an object exists (mirrors getObjectMetadata + handleNotFound).
func (c *S3Client) HeadObject(ctx context.Context, bucket, key string) (exists bool, err error) {
	_, err = c.cl.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PutObject uploads srcFile to key. contentMD5Base64, if non-empty, is sent as
// the Content-MD5 header (mirrors ResolvedS3Repo.put).
func (c *S3Client) PutObject(ctx context.Context, bucket, key, srcFile, contentMD5Base64 string) error {
	f, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer f.Close()
	in := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: f}
	if contentMD5Base64 != "" {
		in.ContentMD5 = aws.String(contentMD5Base64)
	}
	_, err = c.cl.PutObject(ctx, in)
	return err
}

// IsNotFound reports whether an S3 error is a 404 / NoSuchKey / NotFound.
func IsNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket", "404":
			return true
		}
	}
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
		return true
	}
	return false
}
