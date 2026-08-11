export type AccessMode = 'public' | 'pin' | 'password'

export interface Identity {
  id: string
  kind: 'anonymous' | 'telegram' | 'account'
  display_name: string
  telegram_user_id?: number
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
  can_upload: boolean
  status: 'active' | 'deleting'
  accepting_uploads: boolean
  accepting_members: boolean
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
  accepting_members: boolean
  expires_at: string
}

export interface RoomInvitePreview extends RoomPreview {
  permission: 'contributor' | 'viewer'
}

export interface RoomInvite {
  token: string
  permission: 'contributor' | 'viewer'
  created_at: string
  revoked_at?: string
  last_used_at?: string
  join_count: number
}

export interface RoomInviteList {
  invites: RoomInvite[]
  legacy_invites_enabled: boolean
}

export interface RoomMember {
  id: string
  display_name: string
  role: 'owner' | 'member'
  can_upload: boolean
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
  thumbnail_status: 'pending' | 'processing' | 'ready' | 'failed'
  uploaded_by: { id: string; display_name: string }
  favorite_count: number
  favorited: boolean
  is_cover: boolean
  caption: string | null
  caption_updated_at: string | null
  permissions: { can_delete: boolean; can_edit_caption: boolean }
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
  checksum?: string
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

export interface RoomNotificationSettings {
  telegram_available: boolean
  telegram_enabled: boolean
}

export interface RoomRecap {
  media_count: number
  image_count: number
  video_count: number
  member_count: number
  contributor_count: number
  favorite_count: number
  total_bytes: number
  created_at: string
  expires_at: string
}

export type ProblemReportCategory = 'upload' | 'download' | 'room' | 'telegram' | 'other'
export type ProblemReportStatus = 'new' | 'in_progress' | 'resolved' | 'closed'

export interface ProblemReportContext {
  route?: string
  app_build?: string
  platform?: string
  browser?: string
  telegram?: boolean
  online?: boolean
  error_code?: string
  request_id?: string
  occurred_at?: string
}

export interface ProblemReport {
  id: string
  public_id: string
  category: ProblemReportCategory
  description: string
  contact?: string
  technical_context: ProblemReportContext
  status: ProblemReportStatus
  admin_note?: string
  reporter_name?: string
  created_at: string
  updated_at: string
  resolved_at?: string
}

export interface AdminStats {
  active_rooms: number
  ready_media: number
  stored_bytes: number
  total_users: number
  new_reports: number
  reports_today: number
  uploads_today: number
  new_users_today: number
}
