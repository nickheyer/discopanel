export type ModLoader = 'vanilla' | 'forge' | 'fabric' | 'neoforge' | 'paper' | 'spigot' | 
  'bukkit' | 'pufferfish' | 'quilt' | 'magma' | 'magma_maintained' | 'ketting' | 
  'mohist' | 'youer' | 'banner' | 'catserver' | 'arclight' | 'spongevanilla' | 
  'limbo' | 'nanolimbo' | 'crucible' | 'glowstone' | 'custom' | 
  'auto_curseforge' | 'curseforge' | 'ftba' | 'modrinth';
export type ServerStatus = 'stopped' | 'starting' | 'running' | 'stopping' | 'error';

export interface DockerImageInfo {
  tag: string;
  javaVersion: number;
  linux: string;
  jvmType: string;
  archs: string[];
  deprecated: boolean;
  note: string;
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
  memory: number;
  created_at: string;
  updated_at: string;
  last_started: string | null;
  java_version: string;
  docker_image: string;
  data_path: string;
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
  auto_start: boolean;
  proxy_hostname?: string;
}

export interface UpdateServerRequest {
  name?: string;
  description?: string;
  max_players?: number;
  memory?: number;
  mod_loader?: string;
  mc_version?: string;
  java_version?: string;
  docker_image?: string;
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

export interface ServerLogsResponse {
  logs: string;
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