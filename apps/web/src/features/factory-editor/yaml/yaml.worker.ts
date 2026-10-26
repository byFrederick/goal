// monaco-yaml's language service in a worker of its own. Vite bundles a file of ours that
// imports it rather than the package's file directly (monaco-yaml's README, "Why doesn't it
// work with Vite?").
import 'monaco-yaml/yaml.worker.js';
