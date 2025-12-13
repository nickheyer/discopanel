export type ModLoader = 'vanilla' | 'forge' | 'fabric' | 'neoforge' | 'paper' | 'spigot' | 
  'bukkit' | 'pufferfish' | 'quilt' | 'magma' | 'magma_maintained' | 'ketting' | 
  'mohist' | 'youer' | 'banner' | 'catserver' | 'arclight' | 'spongevanilla' | 
  'limbo' | 'nanolimbo' | 'crucible' | 'glowstone' | 'custom' | 
  'auto_curseforge' | 'curseforge' | 'ftba' | 'modrinth' | 'purpur';
export type ServerStatus = 'stopped' | 'starting' | 'running' | 'stopping' | 'error' | 'unhealthy' | 'creating';

export interface DockerImageInfo {
  tag: string;
  java: string;
  distribution: string;
  jvm: string;
  architectures: string[];
  deprecated: boolean;
  notes: string;
  lts: boolean;
  jdk: boolean;
}

export interface AdditionalPort {
  name: string;           // User-friendly name for the port (e.g., "BlueMap Web")
  container_port: number; // Port inside the container
  host_port: number;      // Port on the host machine
  protocol: 'tcp' | 'udp'; // Protocol (defaults to 'tcp' if empty)
}

export interface VolumeMount {
  source: string;           // Host path or volume name
  target: string;           // Container path
  read_only?: boolean;      // Mount as read-only
  type?: 'bind' | 'volume'; // Mount type (defaults to 'bind')
}

export interface DockerOverrides {
  environment?: Record<string, string>;    // Additional environment variables
  volumes?: VolumeMount[];                 // Additional volume mounts
  network_mode?: string;                   // Override network mode
  restart_policy?: string;                 // Override restart policy
  cpu_limit?: number;                      // CPU limit (e.g., 1.5 for 1.5 cores)
  memory_override?: number;                // Override memory limit in MB
  labels?: Record<string, string>;         // Additional labels
  cap_add?: string[];                      // Linux capabilities to add
  cap_drop?: string[];                     // Linux capabilities to drop
  devices?: string[];                      // Device mappings (e.g., "/dev/ttyUSB0:/dev/ttyUSB0")
  extra_hosts?: string[];                  // Extra entries for /etc/hosts
  privileged?: boolean;                    // Run container in privileged mode
  read_only?: boolean;                     // Mount root filesystem as read-only
  security_opt?: string[];                 // Security options
  shm_size?: number;                       // Size of /dev/shm in bytes
  user?: string;                           // User to run commands as
  working_dir?: string;                    // Working directory inside container
  entrypoint?: string[];                   // Override default entrypoint
  command?: string[];                      // Override default command
}

export interface Server {
  id: string;
  name: string;
  description: string;
  mod_loader: ModLoader;
  mc_version: string;
  container_id: string;
  status: ServerStatus;
  port: number;
  proxy_port: number;
  proxy_hostname: string;
  max_players: number;
  players_online: number;
  memory: number; // Allocated memory in MB
  memory_usage?: number; // Current memory usage in MB
  cpu_percent?: number; // Current CPU usage percentage
  disk_usage?: number; // Current disk usage in bytes
  disk_total?: number; // Total disk space available in bytes
  tps?: number;
  tps_command?: string;
  created_at: string;
  updated_at: string;
  last_started: string | null;
  java_version?: string;
  docker_image: string;
  data_path: string;
  detached?: boolean;
  auto_start?: boolean;
  additional_ports?: string;  // JSON string of AdditionalPort[]
  docker_overrides?: string;  // JSON string of DockerOverrides
  host_ip?: string; // Host system IP address for direct connections
}

export interface CreateServerRequest {
  name: string;
  description: string;
  mod_loader: ModLoader;
  mc_version: string;
  port: number;
  max_players: number;
  memory: number;
  docker_image?: string;
  auto_start?: boolean;
  detached?: boolean;
  start_immediately?: boolean;
  proxy_hostname?: string;
  proxy_listener_id?: string;
  use_base_url?: boolean;
  additional_ports?: AdditionalPort[];
  docker_overrides?: DockerOverrides;
}

export interface UpdateServerRequest {
  name?: string;
  description?: string;
  max_players?: number;
  memory?: number;
  mod_loader?: string;
  mc_version?: string;
  docker_image?: string;
  detached?: boolean;
  auto_start?: boolean;
  tps_command?: string;
  additional_ports?: AdditionalPort[];
  docker_overrides?: DockerOverrides;
}

export interface Mod {
  id: string;
  server_id: string;
  name: string;
  file_name: string;
  version: string;
  mod_id: string;
  description: string;
  enabled: boolean;
  uploaded_at: string;
  file_size: number;
}

export interface UpdateModRequest {
  name: string;
  version: string;
  description: string;
  enabled: boolean;
}

export interface FileInfo {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: number;
  is_editable: boolean;
  children?: FileInfo[];
}

export interface MinecraftVersion {
  versions: string[];
  latest: string;
}

export interface ModLoaderInfo {
  Name: string;
  DisplayName: string;
  ModsDirectory: string;
  ConfigDirectory: string;
  SupportedVersions: string[] | null;
  FileExtensions: string[];
}

export interface ApiError {
  error: string;
}

export interface LogEntry {
  Timestamp: string;  // ISO timestamp
  Content: string;    // The actual log line
  Type: string;       // "stdout", "stderr", "command", "command_output"
}

export interface ServerLogsResponse {
  logs: LogEntry[];   // Array of structured log entries
  total: number;      // Total number of entries
}

export interface ServerStatusResponse {
  status: ServerStatus;
}

export interface UploadResponse {
  message: string;
  path: string;
}

export type ServerProperties = Record<string, string>;

export interface ConfigProperty {
  key: string;
  label: string;
  value: any;
  default: any;
  type: string;
  description: string;
  required: boolean;
  system: boolean;
  env_var: string;
  options?: string[];
}

export interface ConfigCategory {
  name: string;
  properties: ConfigProperty[];
}

export interface IndexedModpack {
  id: string;
  indexer_id: string;
  indexer: string;
  name: string;
  slug: string;
  summary: string;
  description: string;
  logo_url: string;
  website_url: string;
  download_count: number;
  categories: string;
  game_versions: string;
  mod_loaders: string;
  latest_file_id: string;
  date_created: string;
  date_modified: string;
  date_released: string;
  updated_at: string;
  indexed_at: string;
  is_favorited?: boolean;
}

export interface ModpackFile {
  id: string;
  modpack_id: string;
  display_name: string;
  file_name: string;
  file_date: string;
  file_length: number;
  release_type: string;
  download_url: string;
  game_versions: string;
  mod_loader: string;
  server_pack_file_id: string | null;
}

export interface ModpackSearchParams {
  q?: string;
  gameVersion?: string;
  modLoader?: string;
  indexer?: string;
  page?: number;
}

export interface ModpackSearchResponse {
  modpacks: IndexedModpack[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ModpackSyncRequest {
  query: string;
  gameVersion: string;
  modLoader: string;
  indexer?: string;
}