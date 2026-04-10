# Bazel Project Instructions

This project uses Bazel as its primary build system, specifically configured for Go using `rules_go` and `gazelle`.

## Common Commands

### 1. Build the Project
To build the entire workspace:
```bash
bazel build //...
```
To build just the main binary:
```bash
bazel build //:mkill
```

### 2. Test the Project
To run all unit tests in the workspace:
```bash
bazel test //...
```

### 3. Run the Application
You can run the application directly through Bazel:
```bash
bazel run //:mkill
```
Alternatively, after building, you can run the generated binary directly:
```bash
./bazel-bin/mkill_/mkill
```

### 4. Update Dependencies (Gazelle & Mod Tidy)
When you add or remove Go imports in your `.go` files, or change `go.mod`, you must update the Bazel build files:
```bash
# Update go.mod/go.sum (using the Bazel-managed Go toolchain)
bazel run @rules_go//go -- mod tidy

# Regenerate BUILD.bazel files
bazel run //:gazelle
```

## Continuous Integration
The project uses GitHub Actions to automatically run `bazel build //...` and `bazel test //...` on pushes and pull requests to the `main` branch.

## PR Validation
Before completing a feature or creating a PR, you MUST ensure that the build and tests always pass. You must fix any build or test errors before considering the task complete.
Each new feature MUST be accompanied by tests that cover the feature code paths.

## Documentation Maintenance

When implementing a new feature, you MUST:
1. Update `ai/spec.md` to reflect the technical specification and core logic of the new feature.
2. Update `README.md` to include a description of the new feature for users.
3. Update the screen recording (e.g. `demo.gif` or `demo.tape`) to showcase the new feature if it includes UI changes. When the `demo.gif` is regenerated one must sanity-check the file size; it should not be zero or very small (e.g., at least 100KB).
4. **Whenever a new keyboard command is added, you MUST update the help pane (`helpView` function in `main.go`) to display the new command.** This is a strict requirement to ensure all commands are documented.

### 5. Formatting Go Code
To format Go files, you MUST use the Bazel-managed Go toolchain instead of your local `gofmt` executable. This ensures the correct formatting version is used across different environments.
```bash
bazel run @rules_go//go -- fmt ARGS
```
