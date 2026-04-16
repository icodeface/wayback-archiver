// TypeScript interfaces for the Wayback archiver

export interface CaptureData {
  url: string;
  title: string;
  html: string;
  frames?: FrameCapture[];
  headers?: Record<string, string>;
  cookies?: CaptureCookie[];
}

export interface CaptureCookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  host_only: boolean;
  secure: boolean;
  http_only: boolean;
  session: boolean;
  same_site?: string;
  expiration_date?: number;
  partition_top_level_site?: string;
}

export interface FrameCapture {
  key: string;
  url: string;
  title: string;
  html: string;
}

export interface ArchiveResponse {
  status: string;
  page_id: number;
  action: 'created' | 'unchanged' | 'updated';
}
