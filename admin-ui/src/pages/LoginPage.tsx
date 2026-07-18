import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { GlyphuxApiError } from "@glyphux/sdk";
import { useAuth } from "@/lib/auth-context";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";

interface LocationState {
  from?: { pathname: string };
}

export function LoginPage() {
  const { user, loading: sessionLoading, login, verifyMfa } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  // Set only when the password step succeeded but the account has MFA
  // enabled — the form then switches to asking for a TOTP/recovery code
  // instead of navigating away.
  const [mfaToken, setMfaToken] = useState<string | undefined>(undefined);
  const [mfaCode, setMfaCode] = useState("");

  if (!sessionLoading && user) {
    const state = location.state as LocationState | null;
    return <Navigate to={state?.from?.pathname ?? "/"} replace />;
  }

  const goHome = () => {
    const state = location.state as LocationState | null;
    navigate(state?.from?.pathname ?? "/", { replace: true });
  };

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      const challenge = await login(email, password);
      if (challenge) {
        setMfaToken(challenge.mfaToken);
      } else {
        goHome();
      }
    } catch (err) {
      setError(err instanceof GlyphuxApiError ? err.message : "Could not sign in. Check your connection and try again.");
    } finally {
      setSubmitting(false);
    }
  };

  const onSubmitMfa = async (e: FormEvent) => {
    e.preventDefault();
    if (!mfaToken) return;
    setSubmitting(true);
    setError(undefined);
    try {
      await verifyMfa(mfaToken, mfaCode);
      goHome();
    } catch (err) {
      setError(err instanceof GlyphuxApiError ? err.message : "Could not verify that code. Try again.");
    } finally {
      setSubmitting(false);
    }
  };

  if (mfaToken) {
    return (
      <div className="flex min-h-svh items-center justify-center p-4">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Two-factor verification</CardTitle>
            <CardDescription>Enter the code from your authenticator app.</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={onSubmitMfa} className="flex flex-col gap-4" noValidate>
              {error && (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="mfa-code">Authentication code</Label>
                <Input
                  id="mfa-code"
                  name="mfa-code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  autoFocus
                  required
                  value={mfaCode}
                  onChange={(e) => setMfaCode(e.target.value)}
                  aria-invalid={!!error}
                />
              </div>
              <Button type="submit" disabled={submitting} className="mt-2">
                {submitting ? <Spinner label="Verifying" className="text-primary-foreground" /> : "Verify"}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Sign in to Glyphux</CardTitle>
          <CardDescription>Enter your admin credentials to continue.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                name="email"
                type="email"
                autoComplete="username"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                aria-invalid={!!error}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-invalid={!!error}
              />
            </div>
            <Button type="submit" disabled={submitting} className="mt-2">
              {submitting ? <Spinner label="Signing in" className="text-primary-foreground" /> : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
