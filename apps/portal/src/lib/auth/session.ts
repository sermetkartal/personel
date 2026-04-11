import { type NextRequest } from "next/server";
import { cookies } from "next/headers";
import { SignJWT, jwtVerify } from "jose";

const SESSION_COOKIE_NAME = "personel_portal_session";
const SESSION_SECRET = process.env["SESSION_SECRET"] ?? "default-dev-secret-32-chars-min!";
const SESSION_MAX_AGE = parseInt(process.env["SESSION_MAX_AGE"] ?? "28800", 10);

export interface SessionPayload {
  userId: string;
  email: string;
  name: string;
  accessToken: string;
  locale: string;
  firstLoginAcknowledged: boolean;
}

function getSecretKey(): Uint8Array {
  return new TextEncoder().encode(SESSION_SECRET);
}

export async function createSession(payload: SessionPayload): Promise<string> {
  const token = await new SignJWT({ ...payload })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(`${SESSION_MAX_AGE}s`)
    .sign(getSecretKey());

  return token;
}

export async function verifySession(token: string): Promise<SessionPayload | null> {
  try {
    const { payload } = await jwtVerify(token, getSecretKey());
    return payload as unknown as SessionPayload;
  } catch {
    return null;
  }
}

export async function getSession(): Promise<SessionPayload | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;

  if (!token) return null;

  return verifySession(token);
}

export async function getSessionFromRequest(
  request: NextRequest
): Promise<SessionPayload | null> {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;

  if (!token) return null;

  return verifySession(token);
}

export { SESSION_COOKIE_NAME, SESSION_MAX_AGE };
