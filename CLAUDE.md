# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This repository is currently empty except for this guidance file. There are no existing build scripts, package manifests, tests, README, Cursor rules, or Copilot instructions to inherit.

## Project goal

The intended project is a traffic attribution tool for monitoring and recording traffic sources while Clash Verge is using TUN mode and/or the system proxy. The practical requirement is to identify which application or service is responsible for observed download volume.

## Development commands

No commands are currently defined. Once implementation begins, add the actual commands here from the chosen project tooling, including:

- how to run the app locally
- how to build/package it
- how to lint/type-check it
- how to run all tests
- how to run a single test

## Architecture notes

No code architecture exists yet. Future implementation should document the chosen design here after the first project structure is established, especially:

- where traffic capture/collection happens
- how traffic is attributed to processes, applications, domains, or services
- how Clash Verge TUN mode and system proxy data sources are integrated
- where usage records are persisted
- how the UI or reporting layer reads aggregated download/upload data
