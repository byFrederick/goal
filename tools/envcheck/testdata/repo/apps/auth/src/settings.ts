// env.COMMENTED is not a read.
export const secret = get('AUTH_SECRET');
export const url = process.env.AUTH_URL;
export const agent = headers.get('user-agent');
