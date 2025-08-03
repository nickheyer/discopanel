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
}

export interface UpdateServerRequest {
  name: string;
  description: string;
  max_players: number;
  memory: number;
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