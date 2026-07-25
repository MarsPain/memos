export const ROUTES = {
  HOME: "/",
  ABOUT: "/about",
  ATTACHMENTS: "/attachments",
  CHAT: "/chat",
  INBOX: "/inbox",
  ARCHIVED: "/archived",
  SEARCH: "/search",
  SHORTCUTS: "/shortcuts",
  SETTING: "/setting",
  EXPLORE: "/explore",
  AUTH: "/auth",
  AUTH_SIGNUP: "/auth/signup",
  AUTH_ADMIN: "/auth/admin",
  AUTH_CALLBACK: "/auth/callback",
  SHARED_MEMO: "/memos/shares",
} as const;

export type RouteKey = keyof typeof ROUTES;
export type RoutePath = (typeof ROUTES)[RouteKey];
