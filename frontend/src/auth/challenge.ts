export type AuthChallengeConfig = { challengeMode?: 'image' | 'turnstile'; turnstileSiteKey?: string };

export function validateAuthChallenge(config: AuthChallengeConfig): AuthChallengeConfig {
  if (config.challengeMode !== 'image' && config.challengeMode !== 'turnstile') {
    throw new Error('Verification configuration is unavailable');
  }
  if (config.challengeMode === 'turnstile' && !config.turnstileSiteKey?.trim()) {
    throw new Error('Cloudflare verification is not configured');
  }
  return config;
}
