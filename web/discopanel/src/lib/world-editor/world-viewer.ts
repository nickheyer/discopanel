import * as THREE from 'three';
import {
  buildChunkMeshForYLevel,
  disposeChunkMesh,
  clearCaches,
  getBlockAt,
  type ChunkMesh
} from './chunk-renderer';
import type { CompactChunk, ChunkData } from '$lib/proto/discopanel/v1/world_pb';

const CHUNK_SIZE = 16;

export interface WorldViewerOptions {
  container: HTMLElement;
  onBlockSelect?: (position: { x: number; y: number; z: number }, button: number) => void;
  onBlockHover?: (position: { x: number; y: number; z: number; blockName: string } | null) => void;
  onSelectionChange?: (
    from: { x: number; y: number; z: number } | null,
    to: { x: number; y: number; z: number } | null
  ) => void;
}

export type EditorTool = 'select' | 'place' | 'replace';

export class WorldViewer {
  private container: HTMLElement;
  private renderer: THREE.WebGLRenderer;
  private scene: THREE.Scene;
  private camera: THREE.OrthographicCamera;
  private chunks: Map<string, ChunkMesh> = new Map();
  private rawChunks: Map<string, CompactChunk | ChunkData> = new Map();
  private mouse: THREE.Vector2;
  private highlightMesh: THREE.Mesh | null = null;
  private selectionMesh: THREE.Mesh | null = null;
  private isSelecting: boolean = false;
  private selectionStart: { x: number; z: number } | null = null;
  private selectionEnd: { x: number; z: number } | null = null;
  private options: WorldViewerOptions;
  private animationId: number | null = null;
  private currentTool: EditorTool = 'select';
  private worldMinY: number = -64;
  private worldMaxY: number = 320;
  private currentYLevel: number = 64;

  // Pan/zoom state
  private isPanning: boolean = false;
  private panStart: { x: number; y: number } = { x: 0, y: 0 };
  private cameraTarget: { x: number; z: number } = { x: 0, z: 0 };
  private zoom: number = 1;
  private readonly MIN_ZOOM = 0.1;
  private readonly MAX_ZOOM = 10;
  private is3DMode: boolean = false;

  // 3D mode camera state
  private cameraAngleX: number = 0;
  private cameraAngleY: number = Math.PI / 4; // 45 degree angle
  private cameraDistance: number = 100;

  constructor(options: WorldViewerOptions) {
    this.options = options;
    this.container = options.container;

    // Initialize renderer
    this.renderer = new THREE.WebGLRenderer({ antialias: false, alpha: false });
    this.renderer.setSize(this.container.clientWidth, this.container.clientHeight);
    this.renderer.setPixelRatio(1); // Force 1:1 for performance
    this.renderer.setClearColor(0x1a1a2e, 1);
    this.container.appendChild(this.renderer.domElement);

    // Initialize scene
    this.scene = new THREE.Scene();

    // Initialize orthographic camera (top-down)
    const aspect = this.container.clientWidth / this.container.clientHeight;
    const viewSize = 128;
    this.camera = new THREE.OrthographicCamera(
      (-viewSize * aspect) / 2,
      (viewSize * aspect) / 2,
      viewSize / 2,
      -viewSize / 2,
      0.1,
      1000
    );
    this.camera.position.set(0, 100, 0);
    this.camera.lookAt(0, 0, 0);
    this.camera.up.set(0, 0, -1); // North is up

    // Initialize mouse tracking
    this.mouse = new THREE.Vector2();

    // Add grid
    this.setupGrid();

    // Event listeners
    this.setupEventListeners();

    // Start animation loop
    this.animate();
  }

  private setupGrid(): void {
    // Chunk grid (16x16 blocks)
    const gridMaterial = new THREE.LineBasicMaterial({ color: 0x333344, transparent: true, opacity: 0.5 });
    const gridGeometry = new THREE.BufferGeometry();
    const gridLines: number[] = [];

    // Draw grid lines every 16 blocks for a large area
    const gridExtent = 256;
    for (let i = -gridExtent; i <= gridExtent; i += CHUNK_SIZE) {
      gridLines.push(-gridExtent, 0.01, i, gridExtent, 0.01, i);
      gridLines.push(i, 0.01, -gridExtent, i, 0.01, gridExtent);
    }

    gridGeometry.setAttribute('position', new THREE.Float32BufferAttribute(gridLines, 3));
    const grid = new THREE.LineSegments(gridGeometry, gridMaterial);
    this.scene.add(grid);

    // Origin axes indicator
    const axisLength = 8;
    const axesGeometry = new THREE.BufferGeometry();
    axesGeometry.setAttribute(
      'position',
      new THREE.Float32BufferAttribute(
        [
          0, 0.02, 0, axisLength, 0.02, 0, // X axis (red)
          0, 0.02, 0, 0, 0.02, axisLength // Z axis (blue)
        ],
        3
      )
    );
    axesGeometry.setAttribute(
      'color',
      new THREE.Float32BufferAttribute(
        [
          1, 0, 0, 1, 0, 0, // Red
          0, 0, 1, 0, 0, 1 // Blue
        ],
        3
      )
    );
    const axesMaterial = new THREE.LineBasicMaterial({ vertexColors: true });
    const axes = new THREE.LineSegments(axesGeometry, axesMaterial);
    this.scene.add(axes);
  }

  private setupEventListeners(): void {
    const canvas = this.renderer.domElement;

    // Resize handler
    const resizeObserver = new ResizeObserver(() => this.handleResize());
    resizeObserver.observe(this.container);

    // Mouse events
    canvas.addEventListener('mousemove', (e) => this.handleMouseMove(e));
    canvas.addEventListener('mousedown', (e) => this.handleMouseDown(e));
    canvas.addEventListener('mouseup', (e) => this.handleMouseUp(e));
    canvas.addEventListener('wheel', (e) => this.handleWheel(e), { passive: false });
    canvas.addEventListener('contextmenu', (e) => e.preventDefault());

    // Touch events for mobile
    canvas.addEventListener('touchstart', (e) => this.handleTouchStart(e), { passive: false });
    canvas.addEventListener('touchmove', (e) => this.handleTouchMove(e), { passive: false });
    canvas.addEventListener('touchend', (e) => this.handleTouchEnd(e));
  }

  private handleResize(): void {
    const width = this.container.clientWidth;
    const height = this.container.clientHeight;

    this.renderer.setSize(width, height);
    this.updateCameraProjection();
  }

  private updateCameraProjection(): void {
    const aspect = this.container.clientWidth / this.container.clientHeight;
    const viewSize = 128 / this.zoom;

    this.camera.left = (-viewSize * aspect) / 2 + this.cameraTarget.x;
    this.camera.right = (viewSize * aspect) / 2 + this.cameraTarget.x;
    this.camera.top = viewSize / 2 + this.cameraTarget.z;
    this.camera.bottom = -viewSize / 2 + this.cameraTarget.z;
    this.camera.updateProjectionMatrix();
  }

  private handleMouseMove(event: MouseEvent): void {
    const rect = this.renderer.domElement.getBoundingClientRect();
    this.mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
    this.mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

    if (this.isPanning) {
      const dx = event.clientX - this.panStart.x;
      const dy = event.clientY - this.panStart.y;

      if (this.is3DMode) {
        // In 3D mode, left-drag rotates camera
        this.cameraAngleX -= dx * 0.01;
        this.cameraAngleY = Math.max(0.1, Math.min(Math.PI / 2 - 0.1, this.cameraAngleY + dy * 0.01));
        this.updateCamera3D();
      } else {
        // In 2D mode, pan the view
        this.cameraTarget.x -= (dx / this.zoom) * 0.5;
        this.cameraTarget.z -= (dy / this.zoom) * 0.5;
        this.updateCameraProjection();
      }

      this.panStart = { x: event.clientX, y: event.clientY };
      return;
    }

    // Update hover position
    const worldPos = this.screenToWorld(event.clientX, event.clientY);
    if (worldPos) {
      const blockX = Math.floor(worldPos.x);
      const blockZ = Math.floor(worldPos.z);

      // Find which chunk this block is in
      const chunkX = Math.floor(blockX / CHUNK_SIZE);
      const chunkZ = Math.floor(blockZ / CHUNK_SIZE);
      const localX = ((blockX % CHUNK_SIZE) + CHUNK_SIZE) % CHUNK_SIZE;
      const localZ = ((blockZ % CHUNK_SIZE) + CHUNK_SIZE) % CHUNK_SIZE;

      const chunkKey = `${chunkX},${chunkZ}`;
      const chunkMesh = this.chunks.get(chunkKey);

      let blockName = 'minecraft:air';
      if (chunkMesh) {
        blockName = getBlockAt(chunkMesh.blockData, localX, this.currentYLevel, localZ, this.worldMinY);
      }

      this.updateHighlight(blockX, blockZ);

      if (this.options.onBlockHover) {
        this.options.onBlockHover({ x: blockX, y: this.currentYLevel, z: blockZ, blockName });
      }

      // Update selection if dragging
      if (this.isSelecting && this.selectionStart) {
        this.selectionEnd = { x: blockX, z: blockZ };
        this.updateSelectionMesh();
      }
    } else {
      this.clearHighlight();
      if (this.options.onBlockHover) {
        this.options.onBlockHover(null);
      }
    }
  }

  private handleMouseDown(event: MouseEvent): void {
    if (event.button === 1 || (event.button === 0 && event.shiftKey)) {
      // Middle click or shift+left click to pan
      this.isPanning = true;
      this.panStart = { x: event.clientX, y: event.clientY };
      this.renderer.domElement.style.cursor = 'grabbing';
      return;
    }

    const worldPos = this.screenToWorld(event.clientX, event.clientY);
    if (!worldPos) return;

    const blockX = Math.floor(worldPos.x);
    const blockZ = Math.floor(worldPos.z);

    if (event.button === 2) {
      // Right click - start selection
      this.isSelecting = true;
      this.selectionStart = { x: blockX, z: blockZ };
      this.selectionEnd = { x: blockX, z: blockZ };
      this.updateSelectionMesh();
    } else if (event.button === 0 && !event.shiftKey) {
      // Left click - block action
      if (this.options.onBlockSelect) {
        this.options.onBlockSelect({ x: blockX, y: this.currentYLevel, z: blockZ }, event.button);
      }
    }
  }

  private handleMouseUp(event: MouseEvent): void {
    if (event.button === 1 || (event.button === 0 && this.isPanning)) {
      this.isPanning = false;
      this.renderer.domElement.style.cursor = 'default';
      return;
    }

    if (event.button === 2) {
      this.isSelecting = false;
      if (this.selectionStart && this.selectionEnd && this.options.onSelectionChange) {
        this.options.onSelectionChange(
          { x: this.selectionStart.x, y: this.currentYLevel, z: this.selectionStart.z },
          { x: this.selectionEnd.x, y: this.currentYLevel, z: this.selectionEnd.z }
        );
      }
    }
  }

  private handleWheel(event: WheelEvent): void {
    event.preventDefault();

    if (this.is3DMode) {
      // In 3D mode, zoom changes camera distance
      const zoomFactor = event.deltaY > 0 ? 1.1 : 0.9;
      this.cameraDistance = Math.max(20, Math.min(500, this.cameraDistance * zoomFactor));
      this.updateCamera3D();
    } else {
      const zoomFactor = event.deltaY > 0 ? 0.9 : 1.1;
      this.zoom = Math.max(this.MIN_ZOOM, Math.min(this.MAX_ZOOM, this.zoom * zoomFactor));
      this.updateCameraProjection();
    }
  }

  private handleTouchStart(event: TouchEvent): void {
    if (event.touches.length === 1) {
      event.preventDefault();
      const touch = event.touches[0];
      this.isPanning = true;
      this.panStart = { x: touch.clientX, y: touch.clientY };
    }
  }

  private handleTouchMove(event: TouchEvent): void {
    if (event.touches.length === 1 && this.isPanning) {
      event.preventDefault();
      const touch = event.touches[0];
      const dx = (touch.clientX - this.panStart.x) / this.zoom;
      const dy = (touch.clientY - this.panStart.y) / this.zoom;
      this.cameraTarget.x -= dx * 0.5;
      this.cameraTarget.z -= dy * 0.5;
      this.panStart = { x: touch.clientX, y: touch.clientY };
      this.updateCameraProjection();
    }
  }

  private handleTouchEnd(_event: TouchEvent): void {
    this.isPanning = false;
  }

  private screenToWorld(screenX: number, screenY: number): { x: number; z: number } | null {
    const rect = this.renderer.domElement.getBoundingClientRect();
    const ndcX = ((screenX - rect.left) / rect.width) * 2 - 1;
    const ndcY = -((screenY - rect.top) / rect.height) * 2 + 1;

    // Convert NDC to world coordinates using orthographic projection
    const aspect = this.container.clientWidth / this.container.clientHeight;
    const viewSize = 128 / this.zoom;

    const worldX = ndcX * ((viewSize * aspect) / 2) + this.cameraTarget.x;
    const worldZ = -ndcY * (viewSize / 2) + this.cameraTarget.z;

    return { x: worldX, z: worldZ };
  }

  private updateHighlight(blockX: number, blockZ: number): void {
    if (!this.highlightMesh) {
      const geometry = new THREE.PlaneGeometry(1, 1);
      const material = new THREE.MeshBasicMaterial({
        color: 0xffffff,
        transparent: true,
        opacity: 0.3,
        side: THREE.DoubleSide
      });
      this.highlightMesh = new THREE.Mesh(geometry, material);
      this.highlightMesh.rotation.x = -Math.PI / 2;
      this.highlightMesh.position.y = 0.02;
      this.scene.add(this.highlightMesh);
    }

    this.highlightMesh.position.x = blockX + 0.5;
    this.highlightMesh.position.z = blockZ + 0.5;
    this.highlightMesh.visible = true;
  }

  private clearHighlight(): void {
    if (this.highlightMesh) {
      this.highlightMesh.visible = false;
    }
  }

  private updateSelectionMesh(): void {
    if (this.selectionMesh) {
      this.scene.remove(this.selectionMesh);
      this.selectionMesh.geometry.dispose();
      (this.selectionMesh.material as THREE.Material).dispose();
      this.selectionMesh = null;
    }

    if (!this.selectionStart || !this.selectionEnd) return;

    const minX = Math.min(this.selectionStart.x, this.selectionEnd.x);
    const maxX = Math.max(this.selectionStart.x, this.selectionEnd.x);
    const minZ = Math.min(this.selectionStart.z, this.selectionEnd.z);
    const maxZ = Math.max(this.selectionStart.z, this.selectionEnd.z);

    const width = maxX - minX + 1;
    const depth = maxZ - minZ + 1;

    const geometry = new THREE.PlaneGeometry(width, depth);
    const material = new THREE.MeshBasicMaterial({
      color: 0x4488ff,
      transparent: true,
      opacity: 0.25,
      side: THREE.DoubleSide
    });

    this.selectionMesh = new THREE.Mesh(geometry, material);
    this.selectionMesh.rotation.x = -Math.PI / 2;
    this.selectionMesh.position.set(minX + width / 2, 0.03, minZ + depth / 2);
    this.scene.add(this.selectionMesh);
  }

  private animate = (): void => {
    this.animationId = requestAnimationFrame(this.animate);
    this.renderer.render(this.scene, this.camera);
  };

  // Public API

  public setTool(tool: EditorTool): void {
    this.currentTool = tool;
  }

  public getTool(): EditorTool {
    return this.currentTool;
  }

  public setYLevel(y: number): void {
    const clampedY = Math.max(this.worldMinY, Math.min(this.worldMaxY, y));
    if (clampedY === this.currentYLevel) return;

    this.currentYLevel = clampedY;
    this.rebuildAllChunks();
  }

  public getYLevel(): number {
    return this.currentYLevel;
  }

  public setWorldYBounds(minY: number, maxY: number): void {
    this.worldMinY = minY;
    this.worldMaxY = maxY;
    this.currentYLevel = Math.max(minY, Math.min(maxY, this.currentYLevel));
  }

  public getWorldYBounds(): { minY: number; maxY: number } {
    return { minY: this.worldMinY, maxY: this.worldMaxY };
  }

  public loadChunks(chunks: (CompactChunk | ChunkData)[]): void {
    for (const chunk of chunks) {
      const key = `${chunk.x},${chunk.z}`;

      // Store raw chunk data
      this.rawChunks.set(key, chunk);

      // Remove existing mesh if present
      const existing = this.chunks.get(key);
      if (existing) {
        this.scene.remove(existing.mesh);
        disposeChunkMesh(existing);
        this.chunks.delete(key);
      }

      // Build mesh for current Y level
      const chunkMesh = buildChunkMeshForYLevel(chunk, this.currentYLevel, this.worldMinY);
      if (chunkMesh) {
        this.scene.add(chunkMesh.mesh);
        this.chunks.set(key, chunkMesh);
      }
    }
  }

  private rebuildAllChunks(): void {
    // Rebuild all chunks for new Y level
    for (const [key, rawChunk] of this.rawChunks) {
      const existing = this.chunks.get(key);
      if (existing) {
        this.scene.remove(existing.mesh);
        disposeChunkMesh(existing);
      }

      const chunkMesh = buildChunkMeshForYLevel(rawChunk, this.currentYLevel, this.worldMinY);
      if (chunkMesh) {
        this.scene.add(chunkMesh.mesh);
        this.chunks.set(key, chunkMesh);
      } else {
        this.chunks.delete(key);
      }
    }
  }

  public removeChunk(x: number, z: number): void {
    const key = `${x},${z}`;
    const chunk = this.chunks.get(key);
    if (chunk) {
      this.scene.remove(chunk.mesh);
      disposeChunkMesh(chunk);
      this.chunks.delete(key);
    }
    this.rawChunks.delete(key);
  }

  public clearChunks(): void {
    this.chunks.forEach((chunk) => {
      this.scene.remove(chunk.mesh);
      disposeChunkMesh(chunk);
    });
    this.chunks.clear();
    this.rawChunks.clear();
  }

  public getSelection(): {
    from: { x: number; y: number; z: number };
    to: { x: number; y: number; z: number };
  } | null {
    if (!this.selectionStart || !this.selectionEnd) return null;
    return {
      from: {
        x: Math.min(this.selectionStart.x, this.selectionEnd.x),
        y: this.currentYLevel,
        z: Math.min(this.selectionStart.z, this.selectionEnd.z)
      },
      to: {
        x: Math.max(this.selectionStart.x, this.selectionEnd.x),
        y: this.currentYLevel,
        z: Math.max(this.selectionStart.z, this.selectionEnd.z)
      }
    };
  }

  public clearSelection(): void {
    this.selectionStart = null;
    this.selectionEnd = null;
    if (this.selectionMesh) {
      this.scene.remove(this.selectionMesh);
      this.selectionMesh.geometry.dispose();
      (this.selectionMesh.material as THREE.Material).dispose();
      this.selectionMesh = null;
    }
    if (this.options.onSelectionChange) {
      this.options.onSelectionChange(null, null);
    }
  }

  public focusOnPosition(x: number, _y: number, z: number): void {
    this.cameraTarget = { x, z };
    this.updateCameraProjection();
  }

  public setZoom(zoom: number): void {
    this.zoom = Math.max(this.MIN_ZOOM, Math.min(this.MAX_ZOOM, zoom));
    this.updateCameraProjection();
  }

  public getZoom(): number {
    return this.zoom;
  }

  public getChunkCount(): number {
    return this.chunks.size;
  }

  public set3DMode(enabled: boolean): void {
    if (this.is3DMode === enabled) return;
    this.is3DMode = enabled;

    if (enabled) {
      // Switch to 3D perspective-like view (using orthographic for simplicity)
      this.updateCamera3D();
    } else {
      // Switch back to 2D top-down
      this.camera.position.set(0, 100, 0);
      this.camera.lookAt(0, 0, 0);
      this.camera.up.set(0, 0, -1);
      this.updateCameraProjection();
    }
  }

  public get3DMode(): boolean {
    return this.is3DMode;
  }

  private updateCamera3D(): void {
    // Position camera in 3D orbit around target
    const x = this.cameraTarget.x + this.cameraDistance * Math.sin(this.cameraAngleX) * Math.cos(this.cameraAngleY);
    const y = this.cameraDistance * Math.sin(this.cameraAngleY);
    const z = this.cameraTarget.z + this.cameraDistance * Math.cos(this.cameraAngleX) * Math.cos(this.cameraAngleY);

    this.camera.position.set(x, y, z);
    this.camera.lookAt(this.cameraTarget.x, 0, this.cameraTarget.z);
    this.camera.up.set(0, 1, 0);
    this.camera.updateProjectionMatrix();
  }

  public dispose(): void {
    if (this.animationId) {
      cancelAnimationFrame(this.animationId);
    }

    this.clearChunks();
    clearCaches();

    if (this.highlightMesh) {
      this.highlightMesh.geometry.dispose();
      (this.highlightMesh.material as THREE.Material).dispose();
    }

    if (this.selectionMesh) {
      this.selectionMesh.geometry.dispose();
      (this.selectionMesh.material as THREE.Material).dispose();
    }

    this.renderer.dispose();

    if (this.renderer.domElement.parentElement) {
      this.renderer.domElement.parentElement.removeChild(this.renderer.domElement);
    }
  }
}
