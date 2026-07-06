# Contact Extension

The Contact extension manages reusable contact forms for a Cannon site.

## What It Provides

- An admin area for creating and managing contact forms.
- A `contact-form` extension page for rendering a selected form from a Cannon route.
- A `contact-form` extension block for rendering a selected form in template block spaces.
- A configuration section for a single Shoutrrr notification URL.
- Extension-owned database tables for forms and submitted entries.
- Embedded templates that can be overridden from the site template directory.

## Installation

When Cannon calls `/install`, the extension creates these namespaced tables:

- `contact_settings`
- `contact_forms`
- `contact_entries`

SQLite sites use Cannon's extension database rules through `extension.OpenDB()`.

## Configuration

Open the extension configuration area in Cannon and set `shoutrrr_url`.

The value can be any Shoutrrr URL supported by `github.com/containrrr/shoutrrr`, such as logger, SMTP/email, Slack, Discord, or another supported service.

## Current Status

The admin form builder, route rendering, block rendering, endpoint submission handling, and notification configuration are in place.
