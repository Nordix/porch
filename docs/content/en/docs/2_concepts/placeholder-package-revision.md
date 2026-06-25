---
title: "Placeholder Package Revision"
type: docs
weight: 4
description: |
  The placeholder package revision tracks the most recent content of a package in its repository. This page explains its behaviour, lifecycle interactions, and how it relates to GitOps workflows.
---

## Overview

The placeholder package revision is a special PackageRevision that Porch creates automatically as a reference to the latest released version of a package. It acts as a "branch-tracking" reference: the placeholder package revision points at the version of the package at the HEAD of the branch of the repository where the package revision is stored.

**Identifying a placeholder:**

| Property | Value |
|----------|-------|
| Revision number | `-1` |
| Workspace name | The branch configured on the Repository CR (`spec.git.branch`), commonly `main` |
| Naming convention | `{repository-name}.{package-name}.{branch-name}` |
| Lifecycle | `Published` |

There is one and only one placeholder package revision per package.

## When Is It Created?

The placeholder package revision is created automatically when the **first revision** of a package is published (transitions from Proposed to Published). You cannot create it manually.

For example, publishing `example-repository.my-package.v1` (revision 1) will also create `example-repository.my-package.main` (revision -1) with the same content.

## When Is It Updated?

Each time a **new revision** of the same package is published, the placeholder is updated to reflect that revision's content and tasks.

For example:
1. Publish v1 (revision 1): placeholder is **created** with v1's content
2. Publish v2 (revision 2): placeholder is **updated** with v2's content

The placeholder **only moves forward** on each explicit publish operations.

## What Happens When the Latest Package Revision Is Deleted?

Deleting the latest published package revision does **not** cause the placeholder to roll back to a previous revision. The placeholder retains the content it had at the time of the last publish. It still refers to the version of the Package Revision at the HEAD of the branch in git.

This is intentional. Consider the scenario:

1. v1 is published (placeholder reflects v1)
2. v2 is published (placeholder reflects v2)
3. v2 is deleted

After step 3, the placeholder **still reflects v2's content**. It does not fall back to v1.

**This occurs because** the deletion of a PackageRevision in Porch:

- Removes the tag/branch reference from Git
- Removes the Porch metadata

However, it does **not** perform a `git revert` on the tracked branch. The actual content on that branch in Git is unchanged. The placeholder remains consistent with the real Git state.

## How to Roll Back a Package

Since the placeholder only moves forward, the intended way to "roll back" is to publish a new revision with the desired content:

1. Copy the older revision to create a new draft:

   ```bash
   porchctl rpkg copy example-repository.my-package.v1 \
     --namespace=default --workspace=v3
   ```

2. Propose and approve it:

   ```bash
   porchctl rpkg propose example-repository.my-package.v3 --namespace=default
   porchctl rpkg approve example-repository.my-package.v3 --namespace=default
   ```

3. The placeholder now reflects v3 (which has v1's content)

This preserves linear Git history and makes the intent **explicit**.

## Relationship to GitOps

In a GitOps workflow, a reconciler (such as Flux or ArgoCD) watches a branch in Git for changes. The placeholder package revision references this branch.

The branch is configured via `spec.git.branch` on the Repository CR. This is commonly `main` but can be any branch name (e.g. `production`, `release`, `staging`). The placeholder's workspace name will match whatever branch is configured.

Because the placeholder mirrors the real state of that branch:

- What the reconciler sees in Git is what the placeholder represents in Porch
- There is no divergence between Porch's view and the deployed state
- Deleting a PackageRevision in Porch does not cause unexpected changes in the deployed environment

## Can the Placeholder Be Deleted?

Yes. The placeholder is **not immutable**. It can be deleted via `porchctl` like any other PackageRevision. This is useful during package cleanup (e.g. removing a package entirely from a repository).

Deleting the placeholder **removes the package content from the configured Git branch**. A commit is made to the branch (e.g. `main`) that deletes the package directory and its files. This means any GitOps reconciler watching that branch will also see the removal.

## Summary

| Action | Effect on Placeholder |
|--------|----------------------|
| First revision published | Placeholder **created** |
| Subsequent revision published | Placeholder **updated** to new content |
| Published revision deleted | Placeholder **unchanged** (no rollback) |
| Placeholder itself deleted | Removed from Porch and content deleted from Git branch |
| New revision published after placeholder deletion | Placeholder **recreated** with contents of the new revision |

## Restrictions

The placeholder cannot be used as a source for certain operations:

- **Clone**: cannot clone from a placeholder
- **Edit/Copy**: cannot edit or copy a placeholder
- **Upgrade**: cannot upgrade a placeholder or use a placeholder as the target upstream of an upgrade

These operations require a specific published revision with a concrete revision number.
