//go:build r2live

package r2_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/storage/r2"
)

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	require.NotEmpty(t, value, key)
	return value
}

func TestRealBucketRoundTrip(t *testing.T) {
	ctx := context.Background()
	endpoint := requiredEnv(t, "R2_ENDPOINT")
	bucket := requiredEnv(t, "R2_BUCKET_NAME")
	storage := r2.New(r2.Settings{
		Endpoint: endpoint, AccessKeyID: requiredEnv(t, "R2_ACCESS_KEY_ID"), SecretKey: requiredEnv(t, "R2_SECRET_ACCESS_KEY"),
		Bucket: bucket, PresignTTL: time.Minute,
	})
	key := "atlas-connection-check/" + uuid.NewString() + ".txt"
	content := []byte("atlas connection check")
	t.Cleanup(func() { assert.NoError(t, storage.Delete(context.Background(), key)) })

	upload, err := storage.PresignUpload(ctx, key, "text/plain", int64(len(content)))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(content))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode, string(body))

	object, err := storage.Stat(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, object)
	assert.EqualValues(t, len(content), object.SizeBytes)
	assert.Equal(t, "text/plain", object.ContentType)

	head, err := storage.FirstBytes(ctx, key, 5)
	require.NoError(t, err)
	assert.Equal(t, "atlas", string(head))

	download, err := storage.PresignDownload(ctx, key)
	require.NoError(t, err)
	got, err := http.Get(download.URL)
	require.NoError(t, err)
	fetched, _ := io.ReadAll(got.Body)
	_ = got.Body.Close()
	assert.Equal(t, content, fetched)

	anonymous, err := http.Get(strings.TrimRight(endpoint, "/") + "/" + bucket + "/" + key)
	require.NoError(t, err)
	_ = anonymous.Body.Close()
	assert.NotEqual(t, http.StatusOK, anonymous.StatusCode, "the S3 endpoint must not serve anonymous reads")

	if public := os.Getenv("R2_PUBLIC_URL"); public != "" {
		exposed, err := http.Get(strings.TrimRight(public, "/") + "/" + key)
		require.NoError(t, err)
		_ = exposed.Body.Close()
		t.Logf("PUBLIC_URL_STATUS=%d", exposed.StatusCode)
	}

	missing, err := storage.Stat(ctx, key+".missing")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
