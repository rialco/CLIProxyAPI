/** Authentication types, migrated from the original login and connection modules. */

// Login credentials.
export interface LoginCredentials {
  apiBase: string;
  managementKey: string;
  rememberPassword?: boolean;
}

// Authentication state.
export interface AuthState {
  isAuthenticated: boolean;
  apiBase: string;
  managementKey: string;
  rememberPassword: boolean;
  serverVersion: string | null;
  serverBuildDate: string | null;
}

// Connection state.
export type ConnectionStatus = 'connected' | 'disconnected' | 'connecting' | 'error';
