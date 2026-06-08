package llm

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// awsCreds holds SigV4 credentials.
type awsCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// loadAWSCreds resolves credentials from the environment first, then from the
// named profile in ~/.aws/credentials.
func loadAWSCreds(profile string) (awsCreds, error) {
	if ak := os.Getenv("AWS_ACCESS_KEY_ID"); ak != "" {
		return awsCreds{
			AccessKeyID:     ak,
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return awsCreds{}, err
	}
	creds, err := parseAWSProfile(filepath.Join(home, ".aws", "credentials"), profile)
	if err != nil {
		return awsCreds{}, err
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return awsCreds{}, fmt.Errorf("profile %q has no aws_access_key_id/aws_secret_access_key in ~/.aws/credentials", profile)
	}
	return creds, nil
}

// parseAWSProfile reads one profile section from an AWS credentials INI file.
func parseAWSProfile(path, profile string) (awsCreds, error) {
	f, err := os.Open(path)
	if err != nil {
		return awsCreds{}, err
	}
	defer f.Close()

	var c awsCreds
	inSection := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = strings.TrimSpace(line[1:len(line)-1]) == profile
			continue
		}
		if !inSection {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "aws_access_key_id":
			c.AccessKeyID = strings.TrimSpace(v)
		case "aws_secret_access_key":
			c.SecretAccessKey = strings.TrimSpace(v)
		case "aws_session_token":
			c.SessionToken = strings.TrimSpace(v)
		}
	}
	return c, sc.Err()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// signV4 signs req in place using AWS Signature Version 4 for the given service
// and region. body is the exact request payload.
func signV4(req *http.Request, body []byte, creds awsCreds, service, region string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	headers := map[string]string{
		"content-type": req.Header.Get("Content-Type"),
		"host":         req.URL.Host,
		"x-amz-date":   amzDate,
	}
	if creds.SessionToken != "" {
		headers["x-amz-security-token"] = creds.SessionToken
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)

	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	payloadHash := sha256hex(body)

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.AccessKeyID, scope, signedHeaders, signature,
	))
}
