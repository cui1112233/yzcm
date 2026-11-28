package novelcreation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAttachmentPrivacy = errors.New("附件存储私有状态无法确认")
var ErrAttachmentObjectNotFound = errors.New("附件原件不存在")
var ErrAttachmentNamespace = errors.New("附件存储命名空间不匹配")

const privateAttachmentMaxBytes = 64 << 10

// Separate client: never changes other document/media object ACL or retries.
type PrivateTOSDocumentStore struct {
	client            *tos.ClientV2
	bucket, namespace string
}

func AttachmentTOSNamespace(c DocumentTOSConfig) (string, error) {
	endpoint := strings.TrimSpace(c.Endpoint)
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || strings.TrimSpace(c.Region) == "" || strings.TrimSpace(c.Bucket) == "" {
		return "", ErrAttachmentNamespace
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimSuffix(host, ":443")
	raw, _ := json.Marshal([]string{"tos-private-attachment-v1", "https://" + host, strings.TrimSpace(c.Region), strings.TrimSpace(c.Bucket)})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}
func NewPrivateTOSDocumentStore(c DocumentTOSConfig) (*PrivateTOSDocumentStore, error) {
	return newPrivateTOSDocumentStore(c, nil)
}
func newPrivateTOSDocumentStore(c DocumentTOSConfig, transport http.RoundTripper) (*PrivateTOSDocumentStore, error) {
	namespace, e := AttachmentTOSNamespace(c)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(c.AccessKey) == "" || strings.TrimSpace(c.SecretKey) == "" {
		return nil, errors.New("附件TOS配置不完整")
	}
	if transport == nil {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	}
	cli, e := tos.NewClientV2(strings.TrimSpace(c.Endpoint), tos.WithRegion(strings.TrimSpace(c.Region)), tos.WithCredentials(tos.NewStaticCredentials(c.AccessKey, c.SecretKey)), tos.WithConnectionTimeout(5*time.Second), tos.WithRequestTimeout(30*time.Second), tos.WithMaxRetryCount(0), tos.WithHTTPTransport(privateACLTransport{transport}))
	if e != nil {
		return nil, errors.New("附件TOS配置无效")
	}
	return &PrivateTOSDocumentStore{cli, strings.TrimSpace(c.Bucket), namespace}, nil
}
func (s *PrivateTOSDocumentStore) NamespaceID() string { return s.namespace }

type privateACLCapture struct{ raw []byte }
type privateACLCaptureKey struct{}
type privateACLTransport struct{ next http.RoundTripper }

func (t privateACLTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, e := t.next.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	if resp == nil || resp.Body == nil {
		return nil, ErrAttachmentPrivacy
	}
	// ACL/policy bodies are small. Preserve presence before the SDK maps omitted
	// bools to false; bounded errors also cannot cause unbounded SDK allocation.
	if r.URL.Query().Has("acl") || r.URL.Query().Has("policy") || resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, privateAttachmentMaxBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(raw) > privateAttachmentMaxBytes {
			return nil, ErrAttachmentPrivacy
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if c, ok := r.Context().Value(privateACLCaptureKey{}).(*privateACLCapture); ok {
			c.raw = raw
		}
	}
	return resp, nil
}
func (s *PrivateTOSDocumentStore) checkPrivacy(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return "", e
	}
	capture := &privateACLCapture{}
	aclCtx := context.WithValue(ctx, privateACLCaptureKey{}, capture)
	if _, e := s.client.GetBucketACL(aclCtx, &tos.GetBucketACLInput{Bucket: s.bucket}); e != nil {
		return "", ErrAttachmentPrivacy
	}
	owner, e := validatePrivateAttachmentACL(capture.raw, false)
	if e != nil {
		return "", e
	}
	policy, e := s.client.GetBucketPolicyV2(ctx, &tos.GetBucketPolicyV2Input{Bucket: s.bucket})
	if e != nil {
		var server *tos.TosServerError
		if !errors.As(e, &server) || server.Code != "NoSuchBucketPolicy" || server.StatusCode != 404 {
			return "", ErrAttachmentPrivacy
		}
	} else if policy == nil || validateEmptyAttachmentPolicy(policy.Policy) != nil {
		return "", ErrAttachmentPrivacy
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	return owner, nil
}
func (s *PrivateTOSDocumentStore) CheckPrivacy(ctx context.Context) error {
	_, e := s.checkPrivacy(ctx)
	return e
}
func validPrivateAttachmentKey(key string) bool {
	if !strings.HasPrefix(key, "assistant-text/") {
		return false
	}
	id := strings.TrimPrefix(key, "assistant-text/")
	if len(id) == 0 || len(id) > 96 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (s *PrivateTOSDocumentStore) validateObject(namespace, key string) error {
	if namespace != s.namespace {
		return ErrAttachmentNamespace
	}
	if !validPrivateAttachmentKey(key) {
		return errors.New("附件对象ID无效")
	}
	return nil
}
func (s *PrivateTOSDocumentStore) PutVerified(ctx context.Context, namespace, key string, raw []byte) error {
	if e := s.validateObject(namespace, key); e != nil {
		return e
	}
	if len(raw) == 0 || len(raw) > privateAttachmentMaxBytes {
		return errors.New("附件大小无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	owner, e := s.checkPrivacy(ctx)
	if e != nil {
		return e
	}
	_, e = s.client.PutObjectV2(ctx, &tos.PutObjectV2Input{PutObjectBasicInput: tos.PutObjectBasicInput{Bucket: s.bucket, Key: key, ContentType: "application/octet-stream", ACL: enum.ACLPrivate}, Content: bytes.NewReader(raw)})
	if e != nil {
		return errors.New("附件原件写入未确认")
	}
	h := sha256.Sum256(raw)
	_, e = s.openVerified(ctx, key, len(raw), hex.EncodeToString(h[:]), owner)
	return e
}
func (s *PrivateTOSDocumentStore) OpenVerified(ctx context.Context, namespace, key string, size int, hash string) ([]byte, error) {
	if e := s.validateObject(namespace, key); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	owner, e := s.checkPrivacy(ctx)
	if e != nil {
		return nil, e
	}
	return s.openVerified(ctx, key, size, hash, owner)
}
func (s *PrivateTOSDocumentStore) openVerified(ctx context.Context, key string, size int, hash, owner string) ([]byte, error) {
	if size < 1 || size > privateAttachmentMaxBytes || len(hash) != 64 {
		return nil, errors.New("附件原件证据无效")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	obj, e := s.client.GetObjectV2(ctx, &tos.GetObjectV2Input{Bucket: s.bucket, Key: key})
	if e != nil {
		var server *tos.TosServerError
		if errors.As(e, &server) && server.Code == "NoSuchKey" && server.StatusCode == 404 {
			return nil, ErrAttachmentObjectNotFound
		}
		return nil, errors.New("附件原件读取未确认")
	}
	if obj == nil || obj.Content == nil {
		return nil, errors.New("附件原件为空")
	}
	raw, e := io.ReadAll(io.LimitReader(obj.Content, privateAttachmentMaxBytes+1))
	obj.Content.Close()
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("附件原件读取未确认")
	}
	h := sha256.Sum256(raw)
	if len(raw) != size || hex.EncodeToString(h[:]) != hash {
		return nil, errors.New("附件原件校验失败")
	}
	capture := &privateACLCapture{}
	if _, e = s.client.GetObjectACL(context.WithValue(ctx, privateACLCaptureKey{}, capture), &tos.GetObjectACLInput{Bucket: s.bucket, Key: key}); e != nil {
		return nil, ErrAttachmentPrivacy
	}
	objectOwner, e := validatePrivateAttachmentACL(capture.raw, true)
	if e != nil || objectOwner != owner {
		return nil, ErrAttachmentPrivacy
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return raw, nil
}

// Unique keys and bounded nesting avoid accepting ambiguous JSON as proof.
func privateJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > privateAttachmentMaxBytes {
		return nil, ErrAttachmentPrivacy
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, e := privateJSONValue(d, 0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrAttachmentPrivacy
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrAttachmentPrivacy
	}
	return m, nil
}
func privateJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, ErrAttachmentPrivacy
	}
	token, e := d.Token()
	if e != nil {
		return nil, e
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return nil, e
			}
			k, ok := key.(string)
			if !ok {
				return nil, ErrAttachmentPrivacy
			}
			if _, exists := m[k]; exists {
				return nil, ErrAttachmentPrivacy
			}
			v, e := privateJSONValue(d, depth+1)
			if e != nil {
				return nil, e
			}
			m[k] = v
		}
		_, e = d.Token()
		return m, e
	case '[':
		a := []any{}
		for d.More() {
			v, e := privateJSONValue(d, depth+1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, e = d.Token()
		return a, e
	}
	return nil, ErrAttachmentPrivacy
}
func privateOnlyKeys(m map[string]any, keys ...string) bool {
	for k := range m {
		found := false
		for _, want := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func privateFalse(m map[string]any, key string) bool { v, ok := m[key].(bool); return ok && !v }
func validatePrivateAttachmentACL(raw []byte, object bool) (string, error) {
	m, e := privateJSON(raw)
	if e != nil {
		return "", ErrAttachmentPrivacy
	}
	if object {
		if !privateOnlyKeys(m, "Owner", "Grants", "BucketOwnerEntrusted", "IsDefault", "VersionId", "VersionID") || !privateFalse(m, "BucketOwnerEntrusted") || !privateFalse(m, "IsDefault") {
			return "", ErrAttachmentPrivacy
		}
	} else if !privateOnlyKeys(m, "Owner", "Grants", "BucketAclDelivered") || !privateFalse(m, "BucketAclDelivered") {
		return "", ErrAttachmentPrivacy
	}
	o, ok := m["Owner"].(map[string]any)
	if !ok || !privateOnlyKeys(o, "ID", "DisplayName") {
		return "", ErrAttachmentPrivacy
	}
	owner, ok := o["ID"].(string)
	if !ok || strings.TrimSpace(owner) == "" {
		return "", ErrAttachmentPrivacy
	}
	grants, ok := m["Grants"].([]any)
	if !ok || len(grants) == 0 {
		return "", ErrAttachmentPrivacy
	}
	full := false
	for _, v := range grants {
		g, ok := v.(map[string]any)
		if !ok || !privateOnlyKeys(g, "Grantee", "Permission") {
			return "", ErrAttachmentPrivacy
		}
		who, ok := g["Grantee"].(map[string]any)
		if !ok || !privateOnlyKeys(who, "Type", "ID", "DisplayName", "Canned") || who["ID"] != owner || who["Type"] != "CanonicalUser" {
			return "", ErrAttachmentPrivacy
		}
		if canned, exists := who["Canned"]; exists && canned != "" {
			return "", ErrAttachmentPrivacy
		}
		permission, ok := g["Permission"].(string)
		if !ok {
			return "", ErrAttachmentPrivacy
		}
		switch permission {
		case "FULL_CONTROL":
			full = true
		case "READ", "READ_ACP", "WRITE", "WRITE_ACP":
		default:
			return "", ErrAttachmentPrivacy
		}
	}
	if !full {
		return "", ErrAttachmentPrivacy
	}
	return owner, nil
}
func validateEmptyAttachmentPolicy(raw string) error {
	m, e := privateJSON([]byte(raw))
	if e != nil || !privateOnlyKeys(m, "Statement", "Version", "Id") {
		return ErrAttachmentPrivacy
	}
	for _, key := range []string{"Version", "Id"} {
		if v, exists := m[key]; exists {
			if _, ok := v.(string); !ok {
				return ErrAttachmentPrivacy
			}
		}
	}
	statements, ok := m["Statement"].([]any)
	if !ok || len(statements) != 0 {
		return ErrAttachmentPrivacy
	}
	return nil
}
