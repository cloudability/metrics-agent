# Contributing to the Metrics Agent

Thank you for contributing!

This document provides some basic guidelines for contributing to this repository.
To propose improvements, feel free to submit a PR.

## Submitting issues

  * If you you have found an issue, please open a Github issue.

## Pull Requests

Here are some items to check when submitting your PR:

  * have a [proper commit history](#commits) (Please rebase if needed).
  * write tests for the code you wrote.
  * preferably make sure that all tests pass locally.
  * summarize your PR with an explanatory title and a message describing your
    changes, cross-referencing any related bugs/PRs.
  * open your PR against the `master` branch.

Pull request must pass all CI tests before we will merge it.

### Keep it focused

Avoid changing too many things at once.

### Commit Messages

Please take a moment to write meaningful commit messages.

The commit message should describe the reason for the change and give extra details
that will allow someone later on to understand in 5 seconds the thing you've been
working on for a day.

### Squash your commits

Please rebase your changes on `master` and squash your commits whenever possible,
it keeps history cleaner and it's easier to revert things.

### Increment the agent version number

Currently the agent version is managed manually in [this file](version.go) please us [Semantic Versioning 2.0.0](https://semver.org/) as your guideline.
