export interface CodexCompatibilityExampleBlock {
  key: 'codexCompatibilityConfig' | 'codexCompatibilityAuth' | 'codexCompatibilityEnv';
  language: 'toml' | 'json' | 'bash';
  display: string;
  real: string;
}

export interface CodexCompatibilityExamples {
  baseUrl: string;
  configToml: CodexCompatibilityExampleBlock;
  authJson: CodexCompatibilityExampleBlock;
  shellEnv: CodexCompatibilityExampleBlock;
}

export function buildCodexCompatibilityExamples({
  serverURL,
  apiKey,
  maskedApiKey,
}: {
  serverURL: string;
  apiKey: string;
  maskedApiKey: string;
}): CodexCompatibilityExamples {
  const baseUrl = `${serverURL}/codex`;
  const configTomlText = `model_provider = "openai"
openai_base_url = ${JSON.stringify(baseUrl)}
chatgpt_base_url = ${JSON.stringify(baseUrl)}
web_search = "live"

[features]
standalone_web_search = true

# Existing threads use the provider ID saved in their history.
# Update this table in place if it already exists.
# Resume with: codex resume -c 'model_provider="OpenAI"' THREAD_ID
[model_providers.OpenAI]
name = "OpenAI"
base_url = ${JSON.stringify(baseUrl)}
wire_api = "responses"
requires_openai_auth = true
supports_websockets = true
supports_standalone_web_search = true`;

  const authDisplay = JSON.stringify(
    {
      personal_access_token: maskedApiKey,
    },
    null,
    2
  );
  const authReal = JSON.stringify(
    {
      personal_access_token: apiKey,
    },
    null,
    2
  );

  const shellEnvText = `# In the same shell that starts Codex (macOS/Linux)
export CODEX_AUTHAPI_BASE_URL=${JSON.stringify(baseUrl)}
# Windows: add CODEX_AUTHAPI_BASE_URL=${baseUrl} in System Environment Variables`;

  return {
    baseUrl,
    configToml: {
      key: 'codexCompatibilityConfig',
      language: 'toml',
      display: configTomlText,
      real: configTomlText,
    },
    authJson: {
      key: 'codexCompatibilityAuth',
      language: 'json',
      display: authDisplay,
      real: authReal,
    },
    shellEnv: {
      key: 'codexCompatibilityEnv',
      language: 'bash',
      display: shellEnvText,
      real: shellEnvText,
    },
  };
}
