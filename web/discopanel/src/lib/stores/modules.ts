import { writable, derived } from 'svelte/store';
import type { Module, ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';
import { ModuleStatus } from '$lib/proto/discopanel/v1/module_pb';
import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
import { create } from '@bufbuild/protobuf';
import {
  ListModulesRequestSchema,
  ListModuleTemplatesRequestSchema,
  GetModuleRequestSchema,
  StartModuleRequestSchema,
  StopModuleRequestSchema,
  RestartModuleRequestSchema,
  DeleteModuleRequestSchema,
  GetModuleLogsRequestSchema
} from '$lib/proto/discopanel/v1/module_pb';

function createModulesStore() {
  const { subscribe, set, update } = writable<Module[]>([]);

  return {
    subscribe,
    fetchModules: async (serverId: string, skipLoading = false) => {
      try {
        const request = create(ListModulesRequestSchema, { serverId });
        const callOptions = skipLoading ? silentCallOptions : undefined;
        const response = await rpcClient.module.listModules(request, callOptions);
        set(response.modules);
        return response.modules;
      } catch (error) {
        console.error('Failed to fetch modules:', error);
        throw error;
      }
    },
    getModule: async (id: string, skipLoading = false) => {
      try {
        const request = create(GetModuleRequestSchema, { id });
        const callOptions = skipLoading ? silentCallOptions : undefined;
        const response = await rpcClient.module.getModule(request, callOptions);
        return response.module;
      } catch (error) {
        console.error('Failed to get module:', error);
        throw error;
      }
    },
    updateModule: (module: Module) => {
      update(modules => {
        const index = modules.findIndex(m => m.id === module.id);
        if (index !== -1) {
          modules[index] = module;
        }
        return modules;
      });
    },
    removeModule: (id: string) => {
      update(modules => modules.filter(m => m.id !== id));
    },
    addModule: (module: Module) => {
      update(modules => [...modules, module]);
    },
    startModule: async (id: string) => {
      try {
        const request = create(StartModuleRequestSchema, { id });
        const response = await rpcClient.module.startModule(request);
        return response.status;
      } catch (error) {
        console.error('Failed to start module:', error);
        throw error;
      }
    },
    stopModule: async (id: string) => {
      try {
        const request = create(StopModuleRequestSchema, { id });
        const response = await rpcClient.module.stopModule(request);
        return response.status;
      } catch (error) {
        console.error('Failed to stop module:', error);
        throw error;
      }
    },
    restartModule: async (id: string) => {
      try {
        const request = create(RestartModuleRequestSchema, { id });
        const response = await rpcClient.module.restartModule(request);
        return response.status;
      } catch (error) {
        console.error('Failed to restart module:', error);
        throw error;
      }
    },
    deleteModule: async (id: string) => {
      try {
        const request = create(DeleteModuleRequestSchema, { id });
        await rpcClient.module.deleteModule(request);
        update(modules => modules.filter(m => m.id !== id));
      } catch (error) {
        console.error('Failed to delete module:', error);
        throw error;
      }
    },
    getLogs: async (id: string, tail: number = 100, skipLoading = false) => {
      try {
        const request = create(GetModuleLogsRequestSchema, { id, tail });
        const callOptions = skipLoading ? silentCallOptions : undefined;
        const response = await rpcClient.module.getModuleLogs(request, callOptions);
        return response.logs;
      } catch (error) {
        console.error('Failed to get module logs:', error);
        throw error;
      }
    },
    clear: () => set([])
  };
}

function createModuleTemplatesStore() {
  const { subscribe, set, update } = writable<ModuleTemplate[]>([]);

  return {
    subscribe,
    fetchTemplates: async (skipLoading = false) => {
      try {
        const request = create(ListModuleTemplatesRequestSchema, { includeCustom: true });
        const callOptions = skipLoading ? silentCallOptions : undefined;
        const response = await rpcClient.module.listModuleTemplates(request, callOptions);
        set(response.templates);
        return response.templates;
      } catch (error) {
        console.error('Failed to fetch module templates:', error);
        throw error;
      }
    },
    addTemplate: (template: ModuleTemplate) => {
      update(templates => [...templates, template]);
    },
    removeTemplate: (id: string) => {
      update(templates => templates.filter(t => t.id !== id));
    },
    clear: () => set([])
  };
}

export const modulesStore = createModulesStore();
export const moduleTemplatesStore = createModuleTemplatesStore();

export const runningModules = derived(
  modulesStore,
  $modules => $modules.filter(module => module.status === ModuleStatus.RUNNING)
);

export const stoppedModules = derived(
  modulesStore,
  $modules => $modules.filter(module => module.status === ModuleStatus.STOPPED)
);

export const builtinTemplates = derived(
  moduleTemplatesStore,
  $templates => $templates.filter(template => template.isBuiltin)
);

export const customTemplates = derived(
  moduleTemplatesStore,
  $templates => $templates.filter(template => !template.isBuiltin)
);
