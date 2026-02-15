export interface ApiErrorShape {
  error: {
    code: string;
    message: string;
    details?: unknown;
  };
}
