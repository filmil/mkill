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

## Documentation Maintenance

When implementing a new feature, you MUST:
1. Update `ai/spec.md` to reflect the technical specification and core logic of the new feature.
2. Update `README.md` to include a description of the new feature for users.
