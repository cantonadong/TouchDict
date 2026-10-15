package chatgpt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const issuer = "https://auth.openai.com"
const resource = "https://api.openai.com/v1"

type Credentials struct {
	ClientID string    `json:"clientId"`
	Subject  string    `json:"subject"`
	Email    string    `json:"email"`
	Access   string    `json:"access"`
	Refresh  string    `json:"refresh"`
	IDToken  string    `json:"idToken"`
	Scope    string    `json:"scope"`
	Expires  time.Time `json:"expires"`
}
type Saved struct {
	Host        string      `json:"host"`
	Credentials Credentials `json:"credentials"`
	Models      []Model     `json:"models"`
}
type Model struct {
	Slug       string `json:"slug"`
	Name       string `json:"display_name"`
	Visibility string `json:"visibility"`
}
type Manager struct {
	mu      sync.Mutex
	saved   Saved
	persist func(Saved) error
}

var Default = &Manager{}
var httpClient = &http.Client{Timeout: 25 * time.Second}

func randomValue() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func (m *Manager) Configure(saved Saved, persist func(Saved) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = saved
	m.persist = persist
}
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.saved.Credentials
	if c.Access == "" {
		return "未连接"
	}
	if time.Now().After(c.Expires) {
		return "已保存连接（查询时自动续期） · " + c.Email
	}
	return "已连接 · " + c.Email
}
func (m *Manager) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved.Credentials.Access != ""
}
func (m *Manager) Models() []Model {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Model(nil), m.saved.Models...)
}
func (m *Manager) saveLocked(s Saved) error {
	if m.persist == nil {
		return errors.New("订阅凭据存储尚未初始化")
	}
	if err := m.persist(s); err != nil {
		return errors.New("无法保存订阅连接")
	}
	m.saved = s
	return nil
}

type tokenResponse struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	IDToken string `json:"id_token"`
	Scope   string `json:"scope"`
	Expires int64  `json:"expires_in"`
}

func exchange(ctx context.Context, form url.Values) (tokenResponse, error) {
	var result tokenResponse
	req, err := http.NewRequestWithContext(ctx, "POST", issuer+"/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return result, errors.New("无法连接 OpenAI 授权服务，请检查网络")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&problem)
		if problem.Error == "invalid_grant" {
			return result, errExpired
		}
		return result, fmt.Errorf("OpenAI 授权失败（HTTP %d），请重新授权", resp.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.Access == "" || result.Expires <= 0 {
		return result, errors.New("OpenAI 授权响应无效")
	}
	return result, nil
}

var errExpired = errors.New("订阅授权已失效，请在设置中重新连接")

func (m *Manager) token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.saved.Credentials
	if c.Access == "" {
		return "", errors.New("请在设置的订阅标签中点击授权")
	}
	if time.Until(c.Expires) > time.Minute {
		return c.Access, nil
	}
	if c.Refresh == "" {
		return "", errExpired
	}
	t, err := exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {c.ClientID}, "refresh_token": {c.Refresh}, "resource": {resource}})
	if err != nil {
		if errors.Is(err, errExpired) {
			s := m.saved
			s.Credentials.Access = ""
			s.Credentials.Refresh = ""
			s.Credentials.IDToken = ""
			_ = m.saveLocked(s)
		}
		return "", err
	}
	s := m.saved
	s.Credentials.Access = t.Access
	s.Credentials.Expires = time.Now().Add(time.Duration(t.Expires) * time.Second)
	if t.Refresh != "" {
		s.Credentials.Refresh = t.Refresh
	}
	if t.IDToken != "" {
		s.Credentials.IDToken = t.IDToken
	}
	if t.Scope != "" {
		if !hasScope(t.Scope, "chatgpt.tokens.use.direct") {
			return "", errExpired
		}
		s.Credentials.Scope = t.Scope
	}
	if err = m.saveLocked(s); err != nil {
		return "", err
	}
	return t.Access, nil
}
func hasScope(scopes, want string) bool {
	for _, s := range strings.Fields(scopes) {
		if s == want {
			return true
		}
	}
	return false
}

type Attempt struct {
	URL                                        string
	result                                     chan callback
	server                                     *http.Server
	redirect, state, nonce, verifier, clientID string
	existing                                   Credentials
}
type callback struct {
	code, clientID string
	err            error
}

func (m *Manager) Begin(ctx context.Context) (*Attempt, error) {
	m.mu.Lock()
	s := m.saved
	if s.Host == "" {
		host, err := randomValue()
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		uuid, _ := base64.RawURLEncoding.DecodeString(host)
		uuid[6] = (uuid[6] & 15) | 64
		uuid[8] = (uuid[8] & 63) | 128
		s.Host = fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
		if err = m.saveLocked(s); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}
	m.mu.Unlock()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("无法启动本地授权回调")
	}
	a := &Attempt{result: make(chan callback, 1), redirect: "http://" + listener.Addr().String() + "/auth/callback", existing: s.Credentials}
	for _, dst := range []*string{&a.state, &a.nonce, &a.verifier} {
		*dst, err = randomValue()
		if err != nil {
			listener.Close()
			return nil, err
		}
	}
	a.clientID = s.Credentials.ClientID
	if a.clientID == "" {
		a.clientID = "dynamic_agent_client"
	}
	challenge := sha256.Sum256([]byte(a.verifier))
	values := url.Values{"client_id": {a.clientID}, "ext_agent_host_id": {s.Host}, "response_type": {"code"}, "redirect_uri": {a.redirect}, "scope": {"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"}, "resource": {resource}, "state": {a.state}, "nonce": {a.nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	if a.clientID == "dynamic_agent_client" {
		values.Set("agent_name_hint", "TouchDict")
	} else if s.Credentials.IDToken != "" {
		values.Set("id_token_hint", s.Credentials.IDToken)
	}
	a.URL = issuer + "/api/accounts/authorize?" + values.Encode()
	mux := http.NewServeMux()
	var once sync.Once
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != "GET" || r.URL.Query().Get("state") != a.state {
			http.Error(w, "授权状态不匹配，请回到 TouchDict 重试。", 400)
			return
		}
		cb := callback{code: r.URL.Query().Get("code"), clientID: r.URL.Query().Get("client_id")}
		if r.URL.Query().Get("error") != "" {
			cb.err = errors.New("授权未完成或已被拒绝，请重试")
		}
		if cb.err == nil && cb.code == "" {
			cb.err = errors.New("授权未返回有效代码")
		}
		sent := false
		once.Do(func() { a.result <- cb; sent = true })
		if !sent {
			http.Error(w, "此授权已处理。", 409)
			return
		}
		fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>TouchDict</title><p>已收到授权结果，请返回 TouchDict 查看连接状态。可以关闭此页面。</p>")
	})
	a.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = a.server.Serve(listener) }()
	go func() {
		select {
		case <-ctx.Done():
			a.Close()
		case <-time.After(5 * time.Minute):
			a.Close()
		}
	}()
	return a, nil
}
func (a *Attempt) Close() { _ = a.server.Close() }
func (m *Manager) Finish(ctx context.Context, a *Attempt) error {
	defer a.Close()
	var cb callback
	select {
	case cb = <-a.result:
	case <-ctx.Done():
		return errors.New("授权已取消或超时，请重试")
	}
	if cb.err != nil {
		return cb.err
	}
	clientID := a.clientID
	if clientID == "dynamic_agent_client" {
		clientID = cb.clientID
		if clientID == "" || clientID == "dynamic_agent_client" {
			return errors.New("OpenAI 未完成应用注册，请重试")
		}
	} else if cb.clientID != "" && cb.clientID != clientID {
		return errors.New("授权账户注册不匹配")
	}
	t, err := exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {cb.code}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirect}, "resource": {resource}})
	if err != nil {
		return err
	}
	identity, err := verifyIdentity(ctx, t.IDToken, clientID, a.nonce)
	if err != nil {
		return err
	}
	if a.existing.Subject != "" && a.existing.Subject != identity.Subject {
		return errors.New("授权账号与已有连接不一致，请先断开连接")
	}
	if !hasScope(t.Scope, "chatgpt.tokens.use.direct") || !hasScope(t.Scope, "resource.invoke") {
		return errors.New("尚未授权使用 ChatGPT 订阅额度，请重新授权")
	}
	if t.Refresh == "" {
		return errors.New("OpenAI 未授予持续连接权限，请重新授权")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s := m.saved
	s.Credentials = Credentials{ClientID: clientID, Subject: identity.Subject, Email: identity.Email, Access: t.Access, Refresh: t.Refresh, IDToken: t.IDToken, Scope: t.Scope, Expires: time.Now().Add(time.Duration(t.Expires) * time.Second)}
	return m.saveLocked(s)
}

type identityClaims struct {
	Issuer   string          `json:"iss"`
	Audience json.RawMessage `json:"aud"`
	Subject  string          `json:"sub"`
	Email    string          `json:"email"`
	Expires  int64           `json:"exp"`
	Nonce    string          `json:"nonce"`
}

func fetchJSON(ctx context.Context, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("无法连接 OpenAI 身份验证服务")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("OpenAI 身份验证服务暂不可用")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}
func verifyIdentity(ctx context.Context, token, clientID, nonce string) (identityClaims, error) {
	var claims identityClaims
	invalid := errors.New("OpenAI 身份验证失败，请重新授权")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, invalid
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	h, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(h, &header) != nil || header.Algorithm != "RS256" {
		return claims, invalid
	}
	var discovery struct {
		Issuer string `json:"issuer"`
		JWKS   string `json:"jwks_uri"`
	}
	if err = fetchJSON(ctx, issuer+"/.well-known/openid-configuration", &discovery); err != nil {
		return claims, err
	}
	u, err := url.Parse(discovery.JWKS)
	if err != nil || u.Scheme != "https" || u.Host != "auth.openai.com" || discovery.Issuer != issuer {
		return claims, invalid
	}
	var keys struct {
		Keys []struct {
			ID   string `json:"kid"`
			Type string `json:"kty"`
			N    string `json:"n"`
			E    string `json:"e"`
		} `json:"keys"`
	}
	if err = fetchJSON(ctx, discovery.JWKS, &keys); err != nil {
		return claims, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims, invalid
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	verified := false
	for _, key := range keys.Keys {
		if key.ID != header.KeyID || key.Type != "RSA" {
			continue
		}
		n, e1 := base64.RawURLEncoding.DecodeString(key.N)
		e, e2 := base64.RawURLEncoding.DecodeString(key.E)
		if e1 != nil || e2 != nil || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, v := range e {
			exponent = exponent*256 + int(v)
		}
		if exponent < 3 {
			continue
		}
		if rsa.VerifyPKCS1v15(&rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, crypto.SHA256, digest[:], sig) == nil {
			verified = true
			break
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if !verified || err != nil || json.Unmarshal(raw, &claims) != nil {
		return claims, invalid
	}
	var aud string
	var audiences []string
	match := false
	if json.Unmarshal(claims.Audience, &aud) == nil {
		match = aud == clientID
	} else if json.Unmarshal(claims.Audience, &audiences) == nil {
		for _, v := range audiences {
			if v == clientID {
				match = true
			}
		}
	}
	if !match || claims.Issuer != issuer || claims.Subject == "" || claims.Nonce != nonce || claims.Expires <= time.Now().Unix() {
		return claims, invalid
	}
	return claims, nil
}
func (m *Manager) Disconnect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.saved
	c := s.Credentials
	var remoteErr error
	if c.Refresh != "" {
		var discovery struct {
			Revocation string `json:"revocation_endpoint"`
		}
		remoteErr = fetchJSON(ctx, issuer+"/.well-known/openid-configuration", &discovery)
		if remoteErr == nil {
			u, e := url.Parse(discovery.Revocation)
			if e != nil || u.Scheme != "https" || u.Host != "auth.openai.com" {
				remoteErr = errors.New("撤销地址无效")
			} else {
				req, _ := http.NewRequestWithContext(ctx, "POST", discovery.Revocation, strings.NewReader(url.Values{"token": {c.Refresh}, "token_type_hint": {"refresh_token"}, "client_id": {c.ClientID}}.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				resp, e := httpClient.Do(req)
				if e != nil {
					remoteErr = e
				} else {
					resp.Body.Close()
					if resp.StatusCode != 200 {
						remoteErr = errors.New("远端撤销未确认")
					}
				}
			}
		}
	}
	s.Credentials.Access = ""
	s.Credentials.Refresh = ""
	s.Credentials.IDToken = ""
	s.Models = nil
	if err := m.saveLocked(s); err != nil {
		return err
	}
	if remoteErr != nil {
		return errors.New("已在本机断开；远端撤销未确认，可到 ChatGPT 设置移除应用授权")
	}
	return nil
}
func (m *Manager) request(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	token, err := m.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, resource+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("无法连接 ChatGPT 订阅服务，请检查网络")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		switch resp.StatusCode {
		case 401:
			return nil, errExpired
		case 403:
			return nil, errors.New("当前账号尚未获准使用此订阅模型")
		case 429:
			return nil, errors.New("订阅额度或请求频率受限，请稍后重试")
		}
		return nil, fmt.Errorf("ChatGPT 订阅请求失败（HTTP %d）", resp.StatusCode)
	}
	return resp, nil
}
