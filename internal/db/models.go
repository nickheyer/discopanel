package db

import (
	"time"
)

type ServerStatus string

const (
	StatusStopped  ServerStatus = "stopped"
	StatusStarting ServerStatus = "starting"
	StatusRunning  ServerStatus = "running"
	StatusStopping ServerStatus = "stopping"
	StatusError    ServerStatus = "error"
)

type ModLoader string

const (
	ModLoaderVanilla  ModLoader = "vanilla"
	ModLoaderForge    ModLoader = "forge"
	ModLoaderFabric   ModLoader = "fabric"
	ModLoaderNeoForge ModLoader = "neoforge"
	ModLoaderPaper    ModLoader = "paper"
	ModLoaderSpigot   ModLoader = "spigot"
)

type Server struct {
	ID          string       `json:"id" gorm:"primaryKey"`
	Name        string       `json:"name" gorm:"not null"`
	Description string       `json:"description"`
	ModLoader   ModLoader    `json:"mod_loader" gorm:"not null"`
	MCVersion   string       `json:"mc_version" gorm:"not null;column:mc_version"`
	ContainerID string       `json:"container_id" gorm:"column:container_id"`
	Status      ServerStatus `json:"status" gorm:"not null"`
	Port        int          `json:"port" gorm:"uniqueIndex"`
	ProxyPort   int          `json:"proxy_port" gorm:"column:proxy_port"`
	MaxPlayers  int          `json:"max_players" gorm:"default:20;column:max_players"`
	Memory      int          `json:"memory" gorm:"default:2048"` // in MB
	CreatedAt   time.Time    `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time    `json:"updated_at" gorm:"autoUpdateTime"`
	LastStarted *time.Time   `json:"last_started" gorm:"column:last_started"`
	JavaVersion string       `json:"java_version" gorm:"column:java_version"`
	DataPath    string       `json:"data_path" gorm:"not null;column:data_path"`
}

type ServerConfig struct {
	ID                 string    `json:"id" gorm:"primaryKey"`
	ServerID           string    `json:"server_id" gorm:"not null;index;column:server_id"`
	Difficulty         string    `json:"difficulty" gorm:"default:'normal'"`
	Gamemode           string    `json:"gamemode" gorm:"default:'survival'"`
	LevelName          string    `json:"level_name" gorm:"default:'world';column:level_name"`
	LevelSeed          string    `json:"level_seed" gorm:"column:level_seed"`
	MaxPlayers         int       `json:"max_players" gorm:"default:20;column:max_players"`
	ViewDistance       int       `json:"view_distance" gorm:"default:10;column:view_distance"`
	OnlineMode         bool      `json:"online_mode" gorm:"default:true;column:online_mode"`
	PVP                bool      `json:"pvp" gorm:"default:true"`
	AllowNether        bool      `json:"allow_nether" gorm:"default:true;column:allow_nether"`
	AllowFlight        bool      `json:"allow_flight" gorm:"default:false;column:allow_flight"`
	SpawnAnimals       bool      `json:"spawn_animals" gorm:"default:true;column:spawn_animals"`
	SpawnMonsters      bool      `json:"spawn_monsters" gorm:"default:true;column:spawn_monsters"`
	SpawnNPCs          bool      `json:"spawn_npcs" gorm:"default:true;column:spawn_npcs"`
	GenerateStructures bool      `json:"generate_structures" gorm:"default:true;column:generate_structures"`
	MOTD               string    `json:"motd" gorm:"default:'A Minecraft Server'"`
	UpdatedAt          time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	Server             *Server   `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
}

type Mod struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	ServerID    string    `json:"server_id" gorm:"not null;index;column:server_id"`
	Name        string    `json:"name" gorm:"not null"`
	FileName    string    `json:"file_name" gorm:"not null;column:file_name"`
	Version     string    `json:"version"`
	ModID       string    `json:"mod_id" gorm:"column:mod_id"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled" gorm:"default:true"`
	UploadedAt  time.Time `json:"uploaded_at" gorm:"autoCreateTime;column:uploaded_at"`
	FileSize    int64     `json:"file_size" gorm:"column:file_size"`
	Server      *Server   `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
}
