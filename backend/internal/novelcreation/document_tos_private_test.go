package novelcreation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type privateRoundTrip func(*http.Request) (*http.Response, error)

func (f privateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const privateBucketACL = `{"Owner":{"ID":"owner"},"Grants":[{"Grantee":{"Type":"CanonicalUser","ID":"owner"},"Permission":"FULL_CONTROL"}],"BucketAclDelivered":false}`
const privateObjectACL = `{"Owner":{"ID":"owner"},"Grants":[{"Grantee":{"Type":"CanonicalUser","ID":"owner"},"Permission":"FULL_CONTROL"}],"BucketOwnerEntrusted":false,"IsDefault":false}`

func privateCfg() DocumentTOSConfig {
	return DocumentTOSConfig{Endpoint: "https://tos.example.test", Region: "cn-test", Bucket: "private-test", AccessKey: "fake-key", SecretKey: "fake-secret"}
}
func privateResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func TestTOSPrivateACLProof(t *testing.T) {
	for _, isObject := range []bool{false, true} {
		base := privateBucketACL
		if isObject {
			base = privateObjectACL
		}
		if _, e := validatePrivateAttachmentACL([]byte(base), isObject); e != nil {
			t.Fatal(e)
		}
		for _, v := range []string{`{}`, `null`, strings.Replace(base, `"ID":"owner"`, `"ID":""`, 1), strings.Replace(base, `"Type":"CanonicalUser"`, `"Type":"Group"`, 1), strings.Replace(base, `"Grants":[`, `"Grants":null,"unused":[`, 1), strings.Replace(base, `:false`, `:null`, 1), strings.Replace(base, `:false`, `:true`, 1), strings.Replace(base, `:false`, `:"false"`, 1)} {
			if _, e := validatePrivateAttachmentACL([]byte(v), isObject); e == nil {
				t.Fatal("unknown accepted", v)
			}
		}
		field := `"BucketAclDelivered":false`
		if isObject {
			field = `"BucketOwnerEntrusted":false,`
		}
		missing := strings.Replace(base, field, "", 1)
		missing = strings.Replace(missing, `,}`, `}`, 1)
		if _, e := validatePrivateAttachmentACL([]byte(missing), isObject); e == nil {
			t.Fatal("missing bool accepted")
		}
	}
}
func TestTOSPrivatePolicyProof(t *testing.T) {
	for _, v := range []string{`{"Statement":[]}`, `{"Version":"2012-10-17","Statement":[]}`} {
		if e := validateEmptyAttachmentPolicy(v); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []string{`null`, `{}`, `{"Statement":null}`, `{"Statement":{}}`, `{"Statement":[{}]}`, `{"Statement":[],"Statement":[{}]}`, `{"Statements":[]}`, `{"Statement":[],"Other":"unknown"}`} {
		if validateEmptyAttachmentPolicy(v) == nil {
			t.Fatal("policy accepted", v)
		}
	}
}
func TestTOSPrivateNamespace(t *testing.T) {
	a, e := AttachmentTOSNamespace(privateCfg())
	if e != nil {
		t.Fatal(e)
	}
	c := privateCfg()
	c.AccessKey = "rotated"
	c.SecretKey = "rotated"
	c.Endpoint = "tos.example.test/"
	b, e := AttachmentTOSNamespace(c)
	if e != nil || a != b {
		t.Fatal("credential rotation", e)
	}
	c.Bucket = "other"
	b, _ = AttachmentTOSNamespace(c)
	if a == b || strings.Contains(a, "fake") {
		t.Fatal("namespace binding")
	}
}
func TestTOSPrivateOriginalTransport(t *testing.T) {
	raw := "hello\r\n中文"
	calls := []string{}
	transport := privateRoundTrip(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("no deadline")
		}
		q := r.URL.Query()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if q.Has("acl") {
			body := privateBucketACL
			if r.URL.Path != "/" {
				body = privateObjectACL
			}
			return privateResponse(r, 200, body), nil
		}
		if q.Has("policy") {
			return privateResponse(r, 404, `{"Code":"NoSuchBucketPolicy","Message":"absent"}`), nil
		}
		if r.Method == "PUT" {
			if r.Header.Get("X-Tos-Acl") != "private" {
				t.Fatal("not private")
			}
			b, _ := io.ReadAll(r.Body)
			if string(b) != raw {
				t.Fatal("Put changed bytes")
			}
			return privateResponse(r, 200, ""), nil
		}
		return privateResponse(r, 200, raw), nil
	})
	s, e := newPrivateTOSDocumentStore(privateCfg(), transport)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PutVerified(context.Background(), s.NamespaceID(), "assistant-text/id", []byte(raw)); e != nil {
		t.Fatal(e, calls)
	}
	if len(calls) != 5 {
		t.Fatal("unexpected retries", calls)
	}
	h := sha256.Sum256([]byte(raw))
	got, e := s.OpenVerified(context.Background(), s.NamespaceID(), "assistant-text/id", len(raw), fmt.Sprintf("%x", h))
	if e != nil || string(got) != raw {
		t.Fatal(string(got), e)
	}
	n := len(calls)
	if e = s.PutVerified(context.Background(), strings.Repeat("0", 64), "assistant-text/id", []byte(raw)); !errors.Is(e, ErrAttachmentNamespace) {
		t.Fatal(e)
	}
	if len(calls) != n {
		t.Fatal("wrong bucket network")
	}
}
func TestTOSPrivateFailureNoRetry(t *testing.T) {
	calls := 0
	s, e := newPrivateTOSDocumentStore(privateCfg(), privateRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return privateResponse(r, 403, `{"Code":"AccessDenied"}`), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	if s.CheckPrivacy(context.Background()) == nil || calls != 1 {
		t.Fatal("failed open/retry", calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if s.PutVerified(ctx, s.NamespaceID(), "assistant-text/id", []byte("x")) == nil {
		t.Fatal("late success")
	}
	if calls != 1 {
		t.Fatal("cancelled network")
	}
}

func TestTOSPrivateUnknownTransportEvidence(t *testing.T) {
	for _, v := range []struct {
		name, bucket, object, policy string
		status                       int
	}{{"empty-policy", privateBucketACL, privateObjectACL, `{"Statement":[]}`, 200}, {"null-policy", privateBucketACL, privateObjectACL, `{"Statement":null}`, 200}, {"generic-404", privateBucketACL, privateObjectACL, `{"Code":"NotFound"}`, 404}, {"403-named-missing", privateBucketACL, privateObjectACL, `{"Code":"NoSuchBucketPolicy"}`, 403}, {"object-missing-bool", privateBucketACL, strings.Replace(privateObjectACL, `,"IsDefault":false`, "", 1), `{"Statement":[]}`, 200}, {"different-owner", privateBucketACL, strings.ReplaceAll(privateObjectACL, "owner", "other"), `{"Statement":[]}`, 200}, {"wrong-original", privateBucketACL, privateObjectACL, `{"Statement":[]}`, 200}} {
		t.Run(v.name, func(t *testing.T) {
			s, e := newPrivateTOSDocumentStore(privateCfg(), privateRoundTrip(func(r *http.Request) (*http.Response, error) {
				q := r.URL.Query()
				if q.Has("acl") {
					a := v.bucket
					if r.URL.Path != "/" {
						a = v.object
					}
					return privateResponse(r, 200, a), nil
				}
				if q.Has("policy") {
					return privateResponse(r, v.status, v.policy), nil
				}
				if r.Method == "PUT" {
					return privateResponse(r, 200, ""), nil
				}
				raw := "hello"
				if v.name == "wrong-original" {
					raw = "wrong"
				}
				return privateResponse(r, 200, raw), nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			e = s.PutVerified(context.Background(), s.NamespaceID(), "assistant-text/id", []byte("hello"))
			if (v.name == "empty-policy") != (e == nil) {
				t.Fatal(v.name, e)
			}
		})
	}
}

func TestTOSPrivateMissingOriginalPrecision(t *testing.T) {
	for _, v := range []struct {
		code    string
		status  int
		missing bool
	}{{"NoSuchKey", 404, true}, {"NotFound", 404, false}, {"NoSuchKey", 403, false}, {"AccessDenied", 403, false}} {
		t.Run(fmt.Sprintf("%s-%d", v.code, v.status), func(t *testing.T) {
			s, e := newPrivateTOSDocumentStore(privateCfg(), privateRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Has("acl") {
					return privateResponse(r, 200, privateBucketACL), nil
				}
				if r.URL.Query().Has("policy") {
					return privateResponse(r, 200, `{"Statement":[]}`), nil
				}
				return privateResponse(r, v.status, fmt.Sprintf(`{"Code":%q}`, v.code)), nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			h := sha256.Sum256([]byte("hello"))
			_, e = s.OpenVerified(context.Background(), s.NamespaceID(), "assistant-text/id", 5, fmt.Sprintf("%x", h))
			if e == nil || errors.Is(e, ErrAttachmentObjectNotFound) != v.missing {
				t.Fatal(e)
			}
		})
	}
}

type privateFailingReader struct{}

func (privateFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("private-object-key-in-reader-error")
}
func TestTOSPrivateOriginalReadErrorIsSafe(t *testing.T) {
	s, e := newPrivateTOSDocumentStore(privateCfg(), privateRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Has("acl") {
			return privateResponse(r, 200, privateBucketACL), nil
		}
		if r.URL.Query().Has("policy") {
			return privateResponse(r, 200, `{"Statement":[]}`), nil
		}
		response := privateResponse(r, 200, "")
		response.Body = io.NopCloser(privateFailingReader{})
		return response, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte("hello"))
	_, e = s.OpenVerified(context.Background(), s.NamespaceID(), "assistant-text/id", 5, fmt.Sprintf("%x", h))
	if e == nil || strings.Contains(e.Error(), "private-object-key") {
		t.Fatal("reader leaked", e)
	}
}
