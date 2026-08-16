import { env } from "cloudflare:workers";
import { Container, getContainer } from "@cloudflare/containers";

// Routes requests into the go-server-mobile Docker container (see
// ../Dockerfile). The Go/Gin app itself is unchanged — this Worker only
// exists because Cloudflare Containers requires one as the entrypoint.
export class MobileBackend extends Container {
  defaultPort = 8080; // matches Dockerfile EXPOSE 8080
  sleepAfter = "10s";

  // Non-sensitive values are plain strings here. Sensitive ones (DB_HOST,
  // DB_USER, DB_PASSWORD, DB_NAME, JWT_KEY) come from `env`, which is only
  // populated after `wrangler secret put <NAME>` — never hardcode those.
  envVars = {
    DB_HOST: env.DB_HOST,
    DB_PORT: "5432",
    DB_USER: env.DB_USER,
    DB_PASSWORD: env.DB_PASSWORD,
    DB_NAME: env.DB_NAME,
    DB_SSLMODE: "require",
    PORT: "8080",
    GIN_MODE: "release",
    JWT_KEY: env.JWT_KEY,
    JWT_NAME: "cocoa_mobile_jwt",
    JWT_ACCESS_TOKEN_EXPIRATION: "3600",
    JWT_REFRESH_TOKEN_EXPIRATION: "86400",
  };
}

interface Env {
  MOBILE_BACKEND: DurableObjectNamespace<MobileBackend>;
}

export default {
  fetch(request: Request, env: Env) {
    const containerInstance = getContainer(env.MOBILE_BACKEND, "default");
    return containerInstance.fetch(request);
  },
};
