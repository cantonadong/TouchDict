use crate::{native, storage};
use anyhow::{Context, Result, bail};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    io::{Read, Write},
    net::TcpListener,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::{Duration, Instant},
};
const ISSUER: &str = "https://auth.openai.com";
const RESOURCE: &str = "https://api.openai.com/v1";
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Credentials {
    pub client_id: String,
    pub subject: String,
    pub email: String,
    pub access: String,
    pub refresh: String,
    pub id_token: String,
    pub scope: String,
    pub expires: String,
}
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct CatalogModel {
    pub slug: String,
    #[serde(rename = "display_name")]
    pub name: String,
    pub visibility: String,
}
#[derive(Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Session {
    pub host: String,
    pub credentials: Credentials,
    pub models: Vec<CatalogModel>,
}
pub struct Auth {
    pub saved: Session,
    http: reqwest::blocking::Client,
}
fn random() -> String {
    let mut b = [0; 32];
    rand::thread_rng().fill_bytes(&mut b);
    URL_SAFE_NO_PAD.encode(b)
}
fn expiration(seconds: u64) -> String {
    (time::OffsetDateTime::now_utc() + time::Duration::seconds(seconds.min(86_400_000) as i64))
        .format(&time::format_description::well_known::Rfc3339)
        .unwrap_or_default()
}
fn expiry(value: &str) -> i64 {
    time::OffsetDateTime::parse(value, &time::format_description::well_known::Rfc3339)
        .map(|v| v.unix_timestamp())
        .unwrap_or(0)
}
fn now() -> i64 {
    time::OffsetDateTime::now_utc().unix_timestamp()
}
impl Auth {
    pub fn load() -> Result<Self> {
        let p = storage::config_dir().join("chatgpt-session.dat");
        let saved = if p.exists() {
            serde_json::from_slice(&native::unprotect(&std::fs::read(p)?)?)?
        } else {
            Session::default()
        };
        Ok(Self {
            saved,
            http: reqwest::blocking::Client::builder()
                .timeout(Duration::from_secs(25))
                .build()?,
        })
    }
    pub fn empty() -> Self {
        Self {
            saved: Session::default(),
            http: reqwest::blocking::Client::new(),
        }
    }
    fn save(&self) -> Result<()> {
        storage::atomic_write(
            &storage::config_dir().join("chatgpt-session.dat"),
            &native::protect(&serde_json::to_vec(&self.saved)?)?,
        )
    }
    pub fn status(&self) -> Value {
        json!({"connected":!self.saved.credentials.access.is_empty(),"email":self.saved.credentials.email,"models":self.saved.models.iter().map(|m|json!({"id":format!("chatgpt:{}",m.slug),"label":m.name})).collect::<Vec<_>>()})
    }
    fn exchange(&self, form: &[(&str, &str)]) -> Result<Value> {
        let response = self
            .http
            .post(format!("{ISSUER}/api/accounts/oauth/token"))
            .form(form)
            .send()
            .context("无法连接 OpenAI 授权服务")?;
        let status = response.status();
        let v: Value = response.json().context("OpenAI 授权响应无效")?;
        if !status.is_success() {
            if v["error"] == "invalid_grant" {
                bail!("订阅授权已失效，请重新连接")
            };
            bail!("OpenAI 授权失败（HTTP {}），请重试", status.as_u16())
        };
        anyhow::ensure!(
            v["access_token"].as_str().is_some_and(|s| !s.is_empty())
                && v["expires_in"].as_u64().is_some_and(|s| s > 0),
            "OpenAI 授权响应缺少凭据"
        );
        Ok(v)
    }
    pub fn token(&mut self) -> Result<String> {
        let c = &self.saved.credentials;
        anyhow::ensure!(!c.access.is_empty(), "请在设置中连接 ChatGPT 订阅");
        if expiry(&c.expires) > now() + 60 {
            return Ok(c.access.clone());
        };
        anyhow::ensure!(!c.refresh.is_empty(), "订阅授权已失效，请重新连接");
        let v = self.exchange(&[
            ("grant_type", "refresh_token"),
            ("client_id", &c.client_id),
            ("refresh_token", &c.refresh),
            ("resource", RESOURCE),
        ])?;
        if let Some(scope) = v["scope"].as_str() {
            anyhow::ensure!(
                scope
                    .split_whitespace()
                    .any(|s| s == "chatgpt.tokens.use.direct"),
                "订阅使用授权已失效"
            );
            self.saved.credentials.scope = scope.into();
        }
        self.saved.credentials.access = v["access_token"].as_str().unwrap_or_default().into();
        self.saved.credentials.expires = expiration(v["expires_in"].as_u64().unwrap_or(3600));
        if let Some(refresh) = v["refresh_token"].as_str() {
            self.saved.credentials.refresh = refresh.into();
        }
        if let Some(id) = v["id_token"].as_str() {
            self.saved.credentials.id_token = id.into();
        }
        self.save()?;
        Ok(self.saved.credentials.access.clone())
    }
    pub fn invalidate(&mut self) {
        self.saved.credentials.expires = expiration(0);
    }
    pub fn begin(&mut self) -> Result<Attempt> {
        if self.saved.host.is_empty() {
            let mut bytes = [0u8; 16];
            rand::thread_rng().fill_bytes(&mut bytes);
            bytes[6] = (bytes[6] & 15) | 64;
            bytes[8] = (bytes[8] & 63) | 128;
            self.saved.host = format!(
                "urn:uuid:{:02x}{:02x}{:02x}{:02x}-{:02x}{:02x}-{:02x}{:02x}-{:02x}{:02x}-{:02x}{:02x}{:02x}{:02x}{:02x}{:02x}",
                bytes[0],
                bytes[1],
                bytes[2],
                bytes[3],
                bytes[4],
                bytes[5],
                bytes[6],
                bytes[7],
                bytes[8],
                bytes[9],
                bytes[10],
                bytes[11],
                bytes[12],
                bytes[13],
                bytes[14],
                bytes[15]
            );
            self.save()?;
        }
        let listener = TcpListener::bind("127.0.0.1:0").context("无法启动本地授权回调")?;
        listener.set_nonblocking(true)?;
        let state = random();
        let nonce = random();
        let verifier = random();
        let redirect = format!("http://{}/auth/callback", listener.local_addr()?);
        let client_id = if self.saved.credentials.client_id.is_empty() {
            "dynamic_agent_client".to_string()
        } else {
            self.saved.credentials.client_id.clone()
        };
        let mut url = reqwest::Url::parse(&format!("{ISSUER}/api/accounts/authorize"))?;
        {
            let mut query = url.query_pairs_mut();
            query.extend_pairs([
                ("client_id", client_id.as_str()),
                ("ext_agent_host_id", &self.saved.host),
                ("response_type", "code"),
                ("redirect_uri", &redirect),
                (
                    "scope",
                    "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct",
                ),
                ("resource", RESOURCE),
                ("state", &state),
                ("nonce", &nonce),
                ("code_challenge_method", "S256"),
                (
                    "code_challenge",
                    &URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes())),
                ),
            ]);
            if client_id == "dynamic_agent_client" {
                query.append_pair("agent_name_hint", "TouchDict");
            } else if !self.saved.credentials.id_token.is_empty() {
                query.append_pair("id_token_hint", &self.saved.credentials.id_token);
            }
        }
        Ok(Attempt {
            url: url.to_string(),
            listener,
            state,
            nonce,
            verifier,
            redirect,
            client_id,
            subject: self.saved.credentials.subject.clone(),
        })
    }
    pub fn finish(&mut self, attempt: Attempt, cancel: &AtomicBool) -> Result<()> {
        let start = Instant::now();
        let mut code = String::new();
        let mut client_id = attempt.client_id.clone();
        while start.elapsed() < Duration::from_secs(300) {
            anyhow::ensure!(!cancel.load(Ordering::Relaxed), "授权已取消");
            match attempt.listener.accept() {
                Ok((mut stream, _)) => {
                    stream.set_read_timeout(Some(Duration::from_secs(2)))?;
                    let mut b = [0u8; 16384];
                    let n = stream.read(&mut b)?;
                    let req = String::from_utf8_lossy(&b[..n]);
                    let target = req
                        .lines()
                        .next()
                        .and_then(|l| l.split_whitespace().nth(1))
                        .unwrap_or("");
                    let url = reqwest::Url::parse(&format!("http://127.0.0.1{target}"))?;
                    let q = url
                        .query_pairs()
                        .into_owned()
                        .collect::<std::collections::HashMap<_, _>>();
                    let valid =
                        url.path() == "/auth/callback" && q.get("state") == Some(&attempt.state);
                    let body = if valid {
                        "<!doctype html><meta charset=utf-8><title>TouchDict</title><p>已收到授权结果，请返回 TouchDict 查看连接状态。可以关闭此页面。</p>"
                    } else {
                        "Invalid callback"
                    };
                    let status = if valid { "200 OK" } else { "400 Bad Request" };
                    let _ = write!(
                        stream,
                        "HTTP/1.1 {status}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nCache-Control: no-store\r\nReferrer-Policy: no-referrer\r\nConnection: close\r\n\r\n{body}",
                        body.len()
                    );
                    if !valid {
                        continue;
                    };
                    anyhow::ensure!(!q.contains_key("error"), "授权已拒绝或取消，请重试");
                    code = q.get("code").cloned().unwrap_or_default();
                    anyhow::ensure!(!code.is_empty(), "未收到有效授权码");
                    if client_id == "dynamic_agent_client" {
                        client_id = q
                            .get("client_id")
                            .cloned()
                            .filter(|v| !v.is_empty() && v != "dynamic_agent_client")
                            .context("OpenAI 未完成应用注册")?;
                    } else if let Some(id) = q.get("client_id") {
                        anyhow::ensure!(id == &client_id, "授权注册不匹配");
                    }
                    break;
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(60))
                }
                Err(e) => return Err(e.into()),
            }
        }
        anyhow::ensure!(!code.is_empty(), "授权等待超时，请重试");
        anyhow::ensure!(!cancel.load(Ordering::Relaxed), "授权已取消");
        let v = self.exchange(&[
            ("grant_type", "authorization_code"),
            ("client_id", &client_id),
            ("code", &code),
            ("code_verifier", &attempt.verifier),
            ("redirect_uri", &attempt.redirect),
            ("resource", RESOURCE),
        ])?;
        let id = v["id_token"].as_str().context("缺少身份凭据")?;
        let claims = self.verify_identity(id, &client_id, &attempt.nonce)?;
        anyhow::ensure!(
            attempt.subject.is_empty() || attempt.subject == claims.sub,
            "授权账号与已有连接不一致"
        );
        let scope = v["scope"].as_str().unwrap_or_default();
        anyhow::ensure!(
            ["chatgpt.tokens.use.direct", "resource.invoke"]
                .iter()
                .all(|wanted| scope.split_whitespace().any(|s| s == *wanted)),
            "未授予订阅查询权限，请重新授权"
        );
        let refresh = v["refresh_token"]
            .as_str()
            .filter(|v| !v.is_empty())
            .context("未授予持续连接权限")?;
        anyhow::ensure!(!cancel.load(Ordering::Relaxed), "授权已取消");
        self.saved.credentials = Credentials {
            client_id,
            subject: claims.sub,
            email: claims.email,
            access: v["access_token"].as_str().unwrap_or_default().into(),
            refresh: refresh.into(),
            id_token: id.into(),
            scope: scope.into(),
            expires: expiration(v["expires_in"].as_u64().unwrap_or(3600)),
        };
        self.save()
    }
    fn verify_identity(&self, token: &str, client_id: &str, nonce: &str) -> Result<Claims> {
        let header = jsonwebtoken::decode_header(token)?;
        anyhow::ensure!(
            header.alg == jsonwebtoken::Algorithm::RS256,
            "身份签名算法无效"
        );
        let discovery: Value = self
            .http
            .get(format!("{ISSUER}/.well-known/openid-configuration"))
            .send()?
            .error_for_status()?
            .json()?;
        anyhow::ensure!(discovery["issuer"] == ISSUER, "身份颁发者无效");
        let uri = discovery["jwks_uri"].as_str().context("缺少签名地址")?;
        trusted(uri)?;
        let jwks: jsonwebtoken::jwk::JwkSet =
            self.http.get(uri).send()?.error_for_status()?.json()?;
        let kid = header.kid.context("缺少身份签名标识")?;
        let key = jwks.find(&kid).context("身份签名密钥不存在")?;
        let decoding = jsonwebtoken::DecodingKey::from_jwk(key)?;
        let mut validation = jsonwebtoken::Validation::new(jsonwebtoken::Algorithm::RS256);
        validation.set_audience(&[client_id]);
        validation.set_issuer(&[ISSUER]);
        validation.leeway = 0;
        let claims = jsonwebtoken::decode::<Claims>(token, &decoding, &validation)?.claims;
        anyhow::ensure!(
            claims.nonce == nonce && !claims.sub.is_empty(),
            "身份验证状态不匹配"
        );
        Ok(claims)
    }
    pub fn refresh_models(&mut self) -> Result<()> {
        let token = self.token()?;
        let v: Value = self
            .http
            .get(format!("{RESOURCE}/models"))
            .bearer_auth(token)
            .send()?
            .error_for_status()?
            .json()?;
        let models: Vec<CatalogModel> = serde_json::from_value(v["models"].clone())?;
        self.saved.models = models
            .into_iter()
            .filter(|m| m.visibility == "list" && !m.slug.is_empty())
            .collect();
        anyhow::ensure!(!self.saved.models.is_empty(), "账号未返回可用订阅模型");
        self.save()
    }
    pub fn disconnect(&mut self) -> Result<()> {
        let mut confirmed = true;
        if !self.saved.credentials.refresh.is_empty() {
            let result = (|| -> Result<()> {
                let discovery: Value = self
                    .http
                    .get(format!("{ISSUER}/.well-known/openid-configuration"))
                    .send()?
                    .error_for_status()?
                    .json()?;
                let uri = discovery["revocation_endpoint"]
                    .as_str()
                    .context("撤销地址不可用")?;
                trusted(uri)?;
                self.http
                    .post(uri)
                    .form(&[
                        ("token", self.saved.credentials.refresh.as_str()),
                        ("token_type_hint", "refresh_token"),
                        ("client_id", self.saved.credentials.client_id.as_str()),
                    ])
                    .send()?
                    .error_for_status()?;
                Ok(())
            })();
            confirmed = result.is_ok();
        }
        self.saved.credentials.access.clear();
        self.saved.credentials.refresh.clear();
        self.saved.credentials.id_token.clear();
        self.saved.models.clear();
        self.save()?;
        if !confirmed {
            bail!("本机已断开；远端撤销未确认，可在 ChatGPT 设置移除授权")
        };
        Ok(())
    }
}
fn trusted(uri: &str) -> Result<()> {
    let u = reqwest::Url::parse(uri)?;
    anyhow::ensure!(
        u.scheme() == "https" && u.host_str() == Some("auth.openai.com"),
        "身份服务地址无效"
    );
    Ok(())
}
#[derive(Clone, Deserialize)]
struct Claims {
    sub: String,
    #[serde(default)]
    email: String,
    nonce: String,
}
pub struct Attempt {
    pub url: String,
    listener: TcpListener,
    state: String,
    nonce: String,
    verifier: String,
    redirect: String,
    client_id: String,
    subject: String,
}
pub type SharedAuth = Arc<std::sync::Mutex<Auth>>;
