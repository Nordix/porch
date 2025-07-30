/*
Copyright 2025 The kpt and Nephio Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
 */

BEGIN;

ALTER TABLE package_revisions
ADD COLUMN ext_repo_state TEXT CHECK (ext_repo_state IN ('NotPushed', 'BeingPushed', 'Pushed', 'Deleting')) NOT NULL DEFAULT 'NotPushed';

ALTER TABLE package_revisions
ADD COLUMN ext_pr_id TEXT NOT NULL DEFAULT '';

ALTER TABLE package_revisions
ALTER COLUMN ext_repo_state DROP DEFAULT;

ALTER TABLE package_revisions
ALTER COLUMN ext_pr_id DROP DEFAULT;

UPDATE package_revisions
SET ext_repo_state = 'NotPushed',
    ext_pr_id = ''
WHERE ext_repo_state IS NULL OR ext_pr_id IS NULL;

COMMIT;
