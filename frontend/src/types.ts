export type AccessMode = 'public' | 'pin' | 'password'

export interface Identity {
  id: string
  kind: 'anonymous' | 'telegram' | 'account'
  display_name: string
}

export interface Session {
  identity: Identity
  access_token: string
  access_token_expires_at: string
  refresh_token: string
  refresh_token_expires_at: string
}

export interface Room {
  id: string
  slug: string
  name: string
  access_mode: AccessMode
  role: 'owner' | 'member'
  status: 'active' | 'deleting'
  accepting_uploads: boolean
  max_files: number
  max_storage_bytes: number
  used_files: number
  used_storage_bytes: number
  created_at: string
  expires_at: string
}

export interface RoomPreview {
  slug: string
  name: string
  access_mode: AccessMode
  status: string
  expires_at: string
}

export interface RoomMember {
  id: string
  display_name: string
  role: 'owner' | 'member'
  joined_at: string
  last_seen_at: string
}

export interface BlockedRoomMember {
  id: string
  display_name: string
  blocked_at: string
}

export interface RoomActivityEvent {
  id: string
  type: 'room_created' | 'member_joined' | 'member_removed' | 'member_unblocked' | 'ownership_transferred' | 'room_updated'
  actor_id: string
  actor_display_name: string
  subject_id?: string
  subject_display_name?: string
  created_at: string
}

export interface GalleryItem {
  id: string
  media_type: 'image' | 'video'
  mime_type: string
  original_filename: string
  size_bytes: number
  width?: number
  height?: number
  duration_ms?: number
  captured_at?: string | null
  created_at: string
  thumbnail_url: string | null
  thumbnail_status: 'pending' | 'ready'
  uploaded_by: { id: string; display_name: string }
  permissions: { can_delete: boolean }
}

export interface GalleryPage {
  items: GalleryItem[]
  next_cursor: string | null
  has_more: boolean
}

export interface UploadTicket {
  id: string
  media_id: string
  type: 'single' | 'multipart'
  status: string
  part_size_bytes?: number
  parts_count?: number
  expires_at: string
  url?: string
  method?: 'PUT'
  headers?: Record<string, string>
}

export interface UploadProgress {
  id: string
  filename: string
  size_bytes: number
  mime_type: string
  last_modified: number
  idempotency_key: string
  upload_id?: string
  completed_parts?: Array<{ part_number: number; etag: string }>
  captured_at?: string | null
  created_at: string
  progress: number
  state: 'queued' | 'uploading' | 'paused' | 'waiting_file' | 'done' | 'error'
  message?: string
  canRetry?: boolean
}

export interface RoomArchive {
  id: string
  room_slug: string
  status: 'pending' | 'processing' | 'ready' | 'failed'
  filename: string
  total_files: number
  processed_files: number
  total_bytes: number
  processed_bytes: number
  size_bytes?: number
  error?: string
  created_at: string
  updated_at: string
  completed_at?: string
}
