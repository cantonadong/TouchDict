use crate::{
    auth::SharedAuth,
    model::{self, Answer, Selection},
    storage::Config,
};
use anyhow::{Context, Result, bail};
use reqwest::blocking::Client;
use serde_json::{Value, json};
use std::{
    io::{BufRead, BufReader},
    time::Duration,
};

pub struct Backend {
    http: Client,
    pub auth: SharedAuth,
}
impl Backend {
    pub fn new(auth: SharedAuth) -> Result<Self> {
        Ok(Self {
            http: Client::builder().timeout(Duration::from_secs(45)).build()?,
            auth,
        })
    }
    pub fn lookup(&self, c: &Config, s: &Selection) -> Result<Answer> {
        let name = c
            .model
            .strip_prefix("chatgpt:")
            .filter(|v| !v.is_empty())
            .context("请先在设置中连接 Codex 并选择模型")?;
        let token = self
            .auth
            .lock()
            .map_err(|_| anyhow::anyhow!("授权服务不可用"))?
            .token()?;
        let body = json!({"model":name,"instructions":model::INSTRUCTIONS,"input":[{"role":"user","content":model::prompt(s)}],"store":false,"stream":true});
        let response = self
            .http
            .post("https://api.openai.com/v1/responses")
            .bearer_auth(&token)
            .json(&body)
            .send()
            .map_err(|error| {
                let timed_out = error.is_timeout();
                let detail = format!("{:#}", error.without_url());
                crate::diagnostics::record(&format!("Codex request failed: {detail}"));
                let lower = detail.to_lowercase();
                let reason = if timed_out {
                    "请求超时"
                } else if lower.contains("dns") || lower.contains("resolve") || lower.contains("11001") {
                    "域名解析失败"
                } else if lower.contains("certificate") || lower.contains("cert") {
                    "HTTPS 证书验证失败"
                } else if lower.contains("10013") || lower.contains("permission denied") || lower.contains("access denied") {
                    "网络访问被阻止，请检查 SimpleWall 的 TouchDict 放行规则"
                } else {
                    "连接失败，请检查网络或 SimpleWall 放行规则"
                };
                anyhow::anyhow!("无法连接 Codex：{reason}")
            })?;
        if !response.status().is_success() {
            if response.status().as_u16() == 401 {
                if let Ok(mut auth) = self.auth.lock() {
                    auth.invalidate();
                }
            }
            return Err(http_error(response.status().as_u16()));
        };
        let mut reader = BufReader::new(std::io::Read::take(response, 4 * 1024 * 1024));
        let mut output = String::new();
        let mut data = String::new();
        let mut line = String::new();
        loop {
            line.clear();
            let bytes = reader.read_line(&mut line)?;
            if bytes == 0 {
                bail!("Codex 连接中断，未收到完整结果，请重试")
            };
            if line.len() > 1_048_576 {
                bail!("Codex 响应过长")
            };
            let line = line.trim_end_matches(['\r', '\n']);
            if let Some(raw) = line.strip_prefix("data:") {
                if !data.is_empty() {
                    data.push('\n')
                };
                data.push_str(raw.trim_start());
            } else if line.is_empty() && !data.is_empty() {
                if data == "[DONE]" {
                    data.clear();
                    continue;
                };
                let v: Value = serde_json::from_str(&data)?;
                data.clear();
                match v["type"].as_str().unwrap_or_default() {
                    "response.output_text.delta" => {
                        output.push_str(v["delta"].as_str().unwrap_or_default())
                    }
                    "response.completed" => {
                        anyhow::ensure!(v["response"]["status"] == "completed", "Codex 响应未完成");
                        if output.is_empty() {
                            for item in v["response"]["output"].as_array().into_iter().flatten() {
                                for content in item["content"].as_array().into_iter().flatten() {
                                    if content["type"] == "output_text" {
                                        output
                                            .push_str(content["text"].as_str().unwrap_or_default());
                                    }
                                }
                            }
                        }
                        return model::parse_answer(&output, s);
                    }
                    "response.failed" | "response.incomplete" | "error" => {
                        bail!("Codex 查询失败，额度受限或结果不完整，请稍后重试")
                    }
                    _ => {}
                }
            }
        }
    }
}
fn http_error(status: u16) -> anyhow::Error {
    anyhow::anyhow!(match status {
        401 => "Codex 授权已失效，请重新连接".to_string(),
        403 => "账号无权使用此模型".to_string(),
        429 => "订阅额度或请求频率受限，请稍后重试".to_string(),
        _ => format!("Codex 响应失败（HTTP {status}）"),
    })
}
