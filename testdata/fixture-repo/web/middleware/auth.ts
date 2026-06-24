export interface AuthContext {
  subject: string;
  scopes: string[];
}

export interface TokenVerifier {
  verify(token: string): AuthContext;
}

export function jwtVerification(token: string, verifier: TokenVerifier): AuthContext {
  return verifier.verify(token);
}

export class JWTAuthMiddleware {
  constructor(private readonly verifier: TokenVerifier) {}

  /** Performs jwt verification on the Authorization header value. */
  verifyHeader(header: string | undefined): AuthContext {
    if (!header) {
      throw new Error("missing authorization header");
    }
    const [scheme, token] = header.split(" ");
    if (scheme?.toLowerCase() !== "bearer" || !token) {
      throw new Error("expected bearer token");
    }
    return this.verifier.verify(token);
  }

  requireScope(ctx: AuthContext, scope: string): void {
    if (!ctx.scopes.includes(scope)) {
      throw new Error(`missing scope: ${scope}`);
    }
  }
}

export function createAuthMiddleware(verifier: TokenVerifier): JWTAuthMiddleware {
  return new JWTAuthMiddleware(verifier);
}

export type ScopeGuard = (ctx: AuthContext) => boolean;

export function hasScope(scope: string): ScopeGuard {
  return (ctx) => ctx.scopes.includes(scope);
}
