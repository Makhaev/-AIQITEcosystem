// Source bytes. JSON/base64 transport overhead is budgeted separately by Go/nginx.
export const MAX_FILE_BYTES = 10 * 1024 * 1024;
export const MAX_ATTACHMENT_BYTES = MAX_FILE_BYTES;
