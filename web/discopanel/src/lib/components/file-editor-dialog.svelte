<script lang="ts">
	import { onMount } from 'svelte';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Dialog as DialogPrimitive } from "bits-ui";
	import { Button } from '$lib/components/ui/button';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import { Loader2, Save, X, Maximize2, Minimize2 } from '@lucide/svelte';
	import type { FileInfo } from '$lib/api/types';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';
	import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker';
	import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
	import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker';

	// Configure Monaco Environment only once globally
	if (!(self as any).MonacoEnvironment) {
		(self as any).MonacoEnvironment = {
			getWorker(_: any, label: string) {
				if (label === 'json') {
					return new jsonWorker();
				}
				if (label === 'css' || label === 'scss' || label === 'less') {
					return new cssWorker();
				}
				if (label === 'html' || label === 'handlebars' || label === 'razor') {
					return new htmlWorker();
				}
				if (label === 'typescript' || label === 'javascript') {
					return new tsWorker();
				}
				return new editorWorker();
			}
		};
	}

	interface Props {
		serverId: string;
		file: FileInfo | null;
		open: boolean;
		onClose: () => void;
		onSave?: () => void;
	}

	let { serverId, file, open = false, onClose, onSave }: Props = $props();

	let content = $state('');
	let originalContent = $state('');
	let loading = $state(false);
	let saving = $state(false);
	let isDirty = $derived(content !== originalContent);
	let isFullscreen = $state(false);
	let editorContainer = $state<HTMLDivElement>();
	let editor = $state<monaco.editor.IStandaloneCodeEditor | null>(null);
	let loadedFilePath = $state<string | null>(null);
	let resizeObserver = $state<ResizeObserver | null>(null);

	// Load file content when dialog opens
	$effect(() => {
		if (open && file && !file.is_dir && file.path !== loadedFilePath) {
			loadedFilePath = file.path;
			loadFileContent();
		}
	});

	// Reset state when dialog closes
	$effect(() => {
		if (!open) {
			content = '';
			originalContent = '';
			isFullscreen = false;
			loadedFilePath = null;
			if (editor) {
				editor.dispose();
				editor = null;
			}
			if (resizeObserver) {
				resizeObserver.disconnect();
				resizeObserver = null;
			}
		}
	});

	// Create editor when content is loaded
	$effect(() => {
		if (open && editorContainer && content !== '' && !editor && !loading) {
			createEditor();
		}
	});

	// Cleanup on unmount
	$effect(() => {
		return () => {
			if (editor) {
				editor.dispose();
			}
			if (resizeObserver) {
				resizeObserver.disconnect();
			}
		};
	});

	async function loadFileContent() {
		if (!file) return;
		
		loading = true;
		try {
			const blob = await api.downloadFile(serverId, file.path);
			const text = await blob.text();
			content = text;
			originalContent = text;
		} catch (error) {
			toast.error('Failed to load file content');
			onClose();
		} finally {
			loading = false;
		}
	}

	async function handleSave() {
		if (!file || !isDirty) return;

		saving = true;
		try {
			await api.updateFile(serverId, file.path, content);
			toast.success('File saved successfully');
			originalContent = content;
			onSave?.();
		} catch (error) {
			toast.error('Failed to save file');
		} finally {
			saving = false;
		}
	}

	function handleClose() {
		if (isDirty) {
			const confirmed = confirm('You have unsaved changes. Are you sure you want to close?');
			if (!confirmed) return;
		}
		onClose();
	}

	function createEditor() {
		if (!editorContainer) return;

		// Determine if file is large (over 100KB)
		const isLargeFile = content.length > 100000;
		
		editor = monaco.editor.create(editorContainer, {
			value: content,
			language: file ? getFileLanguage(file.name) : 'plaintext',
			theme: 'vs-dark',
			fontSize: 14,
			fontFamily: "'JetBrains Mono', 'Monaco', 'Consolas', 'Courier New', monospace",
			minimap: { enabled: !isFullscreen && !isLargeFile },
			scrollBeyondLastLine: false,
			wordWrap: isLargeFile ? 'off' : 'on',
			lineNumbers: 'on',
			renderWhitespace: 'selection',
			bracketPairColorization: { enabled: !isLargeFile },
			formatOnPaste: false, // Disable for performance
			formatOnType: false, // Disable for performance
			automaticLayout: false, // We'll handle resize manually
			fixedOverflowWidgets: true,
			suggest: {
				showWords: !isLargeFile,
				showSnippets: !isLargeFile
			},
			// Additional performance optimizations
			folding: !isLargeFile,
			renderLineHighlight: 'line',
			scrollbar: {
				useShadows: false // Improves scrolling performance
			},
			mouseWheelZoom: false,
			quickSuggestions: !isLargeFile
		});
		
		// Manual resize handling for better performance
		resizeObserver = new ResizeObserver(() => {
			editor?.layout();
		});
		resizeObserver.observe(editorContainer);

		// Add save shortcut
		editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
			handleSave();
		});

		// Track content changes
		editor.onDidChangeModelContent(() => {
			if (editor) {
				content = editor.getValue();
			}
		});

		// Focus editor
		editor.focus();
	}

	function getFileLanguage(fileName: string): string {
		const ext = fileName.toLowerCase().split('.').pop() || '';
		const languageMap: Record<string, string> = {
			'js': 'javascript',
			'ts': 'typescript',
			'jsx': 'javascript',
			'tsx': 'typescript',
			'json': 'json',
			'yml': 'yaml',
			'yaml': 'yaml',
			'toml': 'toml',
			'properties': 'properties',
			'conf': 'conf',
			'cfg': 'ini',
			'ini': 'ini',
			'xml': 'xml',
			'html': 'html',
			'css': 'css',
			'scss': 'scss',
			'sass': 'sass',
			'less': 'less',
			'md': 'markdown',
			'py': 'python',
			'java': 'java',
			'cpp': 'cpp',
			'c': 'c',
			'h': 'c',
			'cs': 'csharp',
			'go': 'go',
			'rs': 'rust',
			'php': 'php',
			'rb': 'ruby',
			'lua': 'lua',
			'sh': 'bash',
			'bash': 'bash',
			'zsh': 'bash',
			'fish': 'bash',
			'ps1': 'powershell',
			'bat': 'batch',
			'cmd': 'batch',
			'dockerfile': 'dockerfile',
			'makefile': 'makefile',
			'gradle': 'groovy',
			'groovy': 'groovy',
			'kt': 'kotlin',
			'swift': 'swift',
			'r': 'r',
			'scala': 'scala',
			'sql': 'sql',
			'pl': 'perl',
			'vim': 'vim'
		};
		return languageMap[ext] || 'plaintext';
	}

	function toggleFullscreen() {
		isFullscreen = !isFullscreen;
		if (editor) {
			editor.updateOptions({
				minimap: { enabled: !isFullscreen }
			});
		}
	}
</script>

<Dialog {open} onOpenChange={(isOpen) => !isOpen && handleClose()}>
	<DialogContent showCloseButton={false} class={isFullscreen ? "!max-w-[95vw] !w-[95vw] h-[95vh] flex flex-col sm:!max-w-[95vw]" : "!max-w-[90vw] !w-[90vw] h-[85vh] flex flex-col sm:!max-w-[90vw]"}>
		<div class="absolute right-4 top-4 flex gap-1">
			<button
				onclick={toggleFullscreen}
				title={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
				class="inline-flex h-9 w-9 items-center justify-center rounded-md text-sm font-medium ring-offset-background transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
			>
				{#if isFullscreen}
					<Minimize2 class="h-4 w-4" />
				{:else}
					<Maximize2 class="h-4 w-4" />
				{/if}
			</button>
			<DialogPrimitive.Close
				class="inline-flex h-9 w-9 items-center justify-center rounded-md text-sm font-medium ring-offset-background transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
			>
				<X class="h-4 w-4" />
				<span class="sr-only">Close</span>
			</DialogPrimitive.Close>
		</div>
		<DialogHeader class="flex-shrink-0">
			<DialogTitle class="flex items-center gap-2">
				{#if file}
					{file.name}
					{#if isDirty}
						<span class="text-sm text-muted-foreground">●</span>
					{/if}
				{:else}
					File Editor
				{/if}
			</DialogTitle>
			<DialogDescription>
				{#if file}
					{file.path}
				{/if}
			</DialogDescription>
		</DialogHeader>
		
		<div class="flex-1 min-h-0 border rounded-md overflow-hidden bg-background relative">
			{#if loading}
				<div class="absolute inset-0 flex items-center justify-center bg-background/80 z-10">
					<Loader2 class="h-8 w-8 animate-spin" />
				</div>
			{/if}
			<div bind:this={editorContainer} class="w-full h-full"></div>
		</div>
		
		<DialogFooter class="flex-shrink-0">
			<div class="flex items-center justify-between w-full">
				<div class="flex items-center gap-4 text-sm text-muted-foreground">
					<span>
						{#if file}
							{getFileLanguage(file.name).toUpperCase()}
						{:else}
							PLAIN TEXT
						{/if}
					</span>
					<span>
						{content.split('\n').length} lines, {content.length} characters
					</span>
					{#if isDirty}
						<span class="text-orange-500">● Modified</span>
					{:else}
						<span class="text-green-500">● Saved</span>
					{/if}
				</div>
				<div class="flex items-center gap-2">
					<span class="text-xs text-muted-foreground">
						Ctrl+S to save
					</span>
					<Button variant="outline" onclick={handleClose}>
						<X class="h-4 w-4 mr-2" />
						Close
					</Button>
					<Button 
						onclick={handleSave} 
						disabled={!isDirty || saving || loading}
						variant={isDirty ? "default" : "secondary"}
					>
						{#if saving}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
						{:else}
							<Save class="h-4 w-4 mr-2" />
						{/if}
						Save
					</Button>
				</div>
			</div>
		</DialogFooter>
	</DialogContent>
</Dialog>