import * as THREE from 'three';
import { getBlockColor } from './blocks';
import type { CompactChunk, ChunkData } from '$lib/proto/discopanel/v1/world_pb';

const CHUNK_SIZE = 16;

export interface ChunkMesh {
  x: number;
  z: number;
  mesh: THREE.Mesh;
  blockData: Record<number, string[]>; // sectionY -> blocks array
}

// Single shared material with vertex colors for maximum performance
let sharedMaterial: THREE.MeshBasicMaterial | null = null;

function getSharedMaterial(): THREE.MeshBasicMaterial {
  if (!sharedMaterial) {
    sharedMaterial = new THREE.MeshBasicMaterial({
      vertexColors: true,
      side: THREE.DoubleSide,
    });
  }
  return sharedMaterial;
}

// Build a single merged mesh for chunk at specific Y level (2D top-down view)
export function buildChunkMeshForYLevel(
  chunk: CompactChunk | ChunkData,
  yLevel: number,
  worldMinY: number = -64
): ChunkMesh | null {
  const blocks = decodeChunkBlocks(chunk);
  const sectionY = Math.floor((yLevel - worldMinY) / 16);
  const localY = ((yLevel - worldMinY) % 16 + 16) % 16;

  const sectionBlocks = blocks[sectionY];
  if (!sectionBlocks) {
    return {
      x: chunk.x,
      z: chunk.z,
      mesh: new THREE.Mesh(new THREE.BufferGeometry(), getSharedMaterial()),
      blockData: blocks,
    };
  }

  const positions: number[] = [];
  const colors: number[] = [];

  for (let lz = 0; lz < 16; lz++) {
    for (let lx = 0; lx < 16; lx++) {
      const idx = localY * 256 + lz * 16 + lx;
      const blockName = sectionBlocks[idx];

      if (!blockName || blockName === 'minecraft:air') continue;

      const colorHex = getBlockColor(blockName);
      const r = ((colorHex >> 16) & 0xff) / 255;
      const g = ((colorHex >> 8) & 0xff) / 255;
      const b = (colorHex & 0xff) / 255;

      // Two triangles for a quad (top-down view)
      positions.push(
        lx, 0, lz,
        lx + 1, 0, lz,
        lx + 1, 0, lz + 1,
        lx, 0, lz,
        lx + 1, 0, lz + 1,
        lx, 0, lz + 1
      );

      for (let v = 0; v < 6; v++) {
        colors.push(r, g, b);
      }
    }
  }

  const geometry = new THREE.BufferGeometry();

  if (positions.length > 0) {
    geometry.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
    geometry.setAttribute('color', new THREE.Float32BufferAttribute(colors, 3));
    geometry.computeBoundingBox();
  }

  const mesh = new THREE.Mesh(geometry, getSharedMaterial());
  mesh.position.set(chunk.x * CHUNK_SIZE, 0, chunk.z * CHUNK_SIZE);

  return {
    x: chunk.x,
    z: chunk.z,
    mesh,
    blockData: blocks,
  };
}

// Decode chunk blocks from either format
export function decodeChunkBlocks(chunk: CompactChunk | ChunkData): Record<number, string[]> {
  const result: Record<number, string[]> = {};

  if ('palette' in chunk && Array.isArray(chunk.palette)) {
    // CompactChunk format
    const compact = chunk as CompactChunk;
    for (const [sectionY, data] of Object.entries(compact.sections)) {
      const blocks: string[] = new Array(4096).fill('minecraft:air');
      const dataArray = data as Uint8Array;
      const maxIdx = Math.min(4096, Math.floor(dataArray.length / 2));
      for (let i = 0; i < maxIdx; i++) {
        const paletteIdx = dataArray[i * 2] | (dataArray[i * 2 + 1] << 8);
        if (paletteIdx < compact.palette.length) {
          blocks[i] = compact.palette[paletteIdx] || 'minecraft:air';
        }
      }
      result[parseInt(sectionY)] = blocks;
    }
  } else if ('sections' in chunk && Array.isArray((chunk as ChunkData).sections)) {
    // Full ChunkData format
    const full = chunk as ChunkData;
    for (const section of full.sections || []) {
      const blocks: string[] = new Array(4096).fill('minecraft:air');
      const palette = section.palette || [];
      const indices = section.paletteIndices || [];

      for (let i = 0; i < Math.min(4096, indices.length); i++) {
        const paletteIdx = indices[i] || 0;
        if (paletteIdx < palette.length) {
          blocks[i] = palette[paletteIdx]?.name || 'minecraft:air';
        }
      }
      result[section.y] = blocks;
    }
  }

  return result;
}

// Get block at position within chunk
export function getBlockAt(
  blockData: Record<number, string[]>,
  localX: number,
  yLevel: number,
  localZ: number,
  worldMinY: number = -64
): string {
  const sectionY = Math.floor((yLevel - worldMinY) / 16);
  const localY = ((yLevel - worldMinY) % 16 + 16) % 16;

  const section = blockData[sectionY];
  if (!section) return 'minecraft:air';

  const idx = localY * 256 + localZ * 16 + localX;
  return section[idx] || 'minecraft:air';
}

// Dispose chunk mesh
export function disposeChunkMesh(chunkMesh: ChunkMesh): void {
  chunkMesh.mesh.geometry.dispose();
}

// Clear caches
export function clearCaches(): void {
  if (sharedMaterial) {
    sharedMaterial.dispose();
    sharedMaterial = null;
  }
}
