# Proposed technical architecture

Project/repository name: `romana-learning`. Display name: Puțin. Keep course levels as content metadata rather than product identity. Future teacher features should use reusable courses/materials, class membership, assignments, and student-owned submissions/progress; those features are not implemented yet.

9 September 2026. Updated after the decision to use Go, Helm, and local Docker Postgres. A local scaffold is implemented; no cloud resources or Kubernetes deployment have been created.

## Stack

- Go HTTP API, React/TypeScript built with Vite, in one repository. Go serves the compiled frontend in a single container.
- Plain CSS for the initial interface; reusable components can grow with the app.
- Standard PostgreSQL with the pgx driver. Docker Compose runs Postgres locally with a persistent named volume.
- Local mode currently has one learner and no login. Choose an authentication provider before the first shared release; Supabase Auth remains an option, not a requirement.
- Remote hosting will use existing Google Cloud compute and an existing PostgreSQL instance, with a separate application database. Course storage supports chart profiles for a persistent node disk/PVC or a GCS bucket; the user's exact resource identifiers are not supplied yet.
- Helm chart 0.2.0 configures database host/name/TLS, Secret keys, course volumes/bucket settings, Google identities, and node placement. It provisions no server/database/bucket/node. Kubernetes is required for Helm; a standalone VM would use container deployment. No cluster is required for local use.
- AI and speech providers behind server endpoints; select providers after checking Romanian quality, latency, and cost. Keep credentials server-side and apply per-user quotas.

Docker Compose starts both services locally. The Helm chart takes an image and an existing database Secret, with resource limits and health probes. Ingress is scaffolded but disabled until authentication exists. See [README.md](README.md) for commands. The following multi-user requirements describe later work, not features already shipped.

## Persistence

Postgres is the authoritative store for saved attempts, mistake history, per-skill review state, completed sessions, achievements, and preferences that should follow a user across devices. Save during a session, not just at its end. Show pending versus saved status honestly.

The current local mode uses no cookies. Future authentication may use session cookies or provider tokens; choose and document the flow when the provider is selected. Learning history remains in Postgres, and the Go server must validate identity and authorize each operation.

Browser localStorage is for small device preferences such as muted sound. Use IndexedDB for drafts and a bounded queue of unsynced attempts if connection recovery is included. Namespace local data by account and keep it inaccessible to another signed-in account. Clearing browser data can remove unsynced work; it does not remove progress already saved to the server.

Give each attempt a unique client-generated identifier, enforce uniqueness per user, and calculate rewards/review updates transactionally so retries cannot double-count. Sync attempts rather than overwriting aggregate progress from an old device. Full offline lessons and offline AI are outside the initial release.

## Sharing and access

Deploy a stable production URL, then share that link. Each learner signs in to a separate account with independent progress. Start invite-only while experimenting with AI usage. Sharing the application does not expose the owner's notes, recordings, homework, or learning history.

Content has explicit visibility: an authored shared course, a personal course, or a course shared with selected members. New uploaded notes default to private. Apply database row-level security to learner records and course membership, and storage policies to private files. Do not depend on hiding buttons to enforce access. See [Supabase row-level security](https://supabase.com/docs/guides/database/postgres/row-level-security).

In the first shared release, a friend should be able to use an authored shared starter course and their own learning record. That release is not implemented yet. A teacher view or sharing a progress report is a separate optional feature with explicit access. Public signup, social features, and organization administration can wait.

## Course library and imports (planned)

The running demo still uses five exercises defined in Go. Uploading, extraction, and course management are the next content features; no local course documents have been uploaded remotely.

Store original DOCX/PDF/audio files in private file storage, with stable object identifiers. Store document metadata, hashes, source versions, extracted text, lesson organization, review status, and published exercises in Postgres. Keep original documents outside application images and Git. A remote copy allows practice without the learner's laptop being online.

Use a Go storage interface with a filesystem implementation for local development and persistent-disk deployments, and a GCS implementation if using a bucket. File access always goes through authorization or authorized download URLs. Helm volume/bucket/identity wiring is implemented; the file API and storage adapters remain to be implemented. The current Compose configuration still contains only the database volume. Kubernetes persistent storage has a lifecycle separate from pods: [persistent volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).

Start with a Course library screen supporting multi-file upload, lesson assignment, and a preview before publishing. Workflow: upload → extract → propose exercises → review/edit → publish. Group lesson notes, blank homework, learner submissions, and teacher corrections under one lesson while preserving their distinct roles. Student answers are not answer keys. Flag uncertain extraction and contradictions for review. Browser uploads copy explicitly selected files; the remote app cannot read arbitrary files from the Mac.

Uploading changed notes creates a new document revision. Compare content hashes to avoid importing identical files repeatedly, within the owner's library. Keep the published course usable while its replacement is in draft. Give skills stable identifiers and exercises immutable revisions; previous attempts retain the revision and feedback they used. A new upload does not reset progress or silently regrade old attempts. Retire superseded exercises from future queues while preserving their history. Object-storage versioning may add recovery protection, but application revisions remain explicit; [S3 versioning](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Versioning.html) is one provider's implementation.

Later, offer an explicit local sync command for the selected course folder: preview added/changed files, then upload them using an authenticated API. Default to one-way imports without propagating deletions. A missing local file must not automatically delete remote lessons. No background watcher is required initially.

Notes and personal homework default to private. Sharing a practice course and sharing its original source files are separate permissions. Back up both Postgres and file storage, and provide an export containing originals plus a manifest of lessons and revisions. Moving hosts should require migrating these stores and updating configuration, rather than rebuilding the course manually.

## Minimal data model

- profiles: user preferences and timezone
- courses, course_memberships, lessons, exercises: versioned content and access
- attempts: answers, assistance used, feedback, exercise version, timestamps
- review_state: user/skill scheduling and demonstrated ability
- practice_sessions and achievements: completion and rewards
- media_assets: owner, visibility, private storage reference, retention preference

Store timestamps consistently and calculate daily consistency using the user's chosen timezone. Allow progress export and account-data deletion. Configure backups appropriate to the selected hosting plan before relying on the service for irreplaceable history; do not assume a free plan provides a particular backup policy.

## Delivery order

1. Build one local end-to-end mission with seeded content and a persistence interface.
2. Add cloud authentication, per-user storage, and access rules before sharing the first usable release.
3. Verify save/resume, two-account isolation, duplicate-request handling, and private-file access.
4. Deploy to a production URL with separate preview configuration; verify on phone and desktop.
5. Add richer speech/AI and connection recovery after the basic loop works.

The initial UI prototype can use local data, but a shareable release must save progress in the database. Provider setup, costs, and deployment are future concrete actions; no subscriptions or external changes are part of this proposal.
