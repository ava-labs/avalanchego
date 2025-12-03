# Welcome contributors

We are eager for contributions and happy you found yourself here.
Please read through this document to familiarize yourself with our
guidelines for contributing to firewood.

## Table of Contents

* [Quick Links](#Quick Links)
* [Testing](#testing)
* [How to submit changes](#How to submit changes)
* [Code Review Process](#Code Review Process)
* [Where can I ask for help?](#Where can I ask for help)

## [Quick Links]

* [Setting up docker](README.docker.md)
* [Auto-generated documentation](https://ava-labs.github.io/firewood/firewood/)
* [Issue tracker](https://github.com/ava-labs/firewood/issues)

## [Testing]

After submitting a PR, we'll run all the tests and verify your code meets our submission guidelines. To ensure it's more likely to pass these checks, you should run the following commands locally:

    cargo fmt
    cargo test
    cargo clippy
    cargo doc --no-deps

Resolve any warnings or errors before making your PR.

## [How to submit changes]

To create a PR, fork firewood, and use GitHub to create the PR. We typically prioritize reviews in the middle of the next work day,
so you should expect a response during the week within 24 hours.

## [Code Review Process]

Code review is a critical part of our development process. It ensures that our codebase remains maintainable, performant, and secure. This document outlines how we approach code reviews at Ava Labs, with responsibilities and expectations for both reviewers and authors.

### For Reviewers

Reviews should be completed or commented within one business day. We have a daily reminder for reviews that have not been reviewed that is posted in slack's #firewood channel.

When reviewing code, your goal is to help the author improve the quality of the change and confirm that it meets our architectural and operational standards. GitHub provides three primary review options:

#### ✅ Accept (Approve)

Use this when the code is an improvement over the current state of the codebase.

* It's okay to request minor changes in comments and still approve the pull request.
* Perfection is not the goal — progress is. If the submitted code is better than what's in production, it's acceptable to approve even if small improvements remain. Consider adding a new issue or request adding a code TODO for larger changes.

#### 💬 Comment (Comment Only)

Use this when your review is incomplete, or you're not ready to approve or reject yet. You should use this if the code is too large to review in a limited amount of time (typically 30-60 minutes). You can also suggest how to break up this diff into a smaller diff.

* This can be helpful for asking clarifying questions, suggesting optional improvements, or flagging issues you're unsure about.
* This state signals that your review is in progress or advisory, not final.

#### ❌ Reject (Request Changes)

Use this when there are significant concerns with the code's correctness, architecture, design, or maintainability.

* A "Reject" signals that the pull request must not be merged until the raised issues are addressed.
* The author is expected to make substantial revisions and return the code for a second round of review by the same reviewer.

#### Best Practices

* Be respectful and constructive. Your comments should guide and empower the author, not discourage them.
* Justify your feedback with principles, not preferences.
* If you're unsure, ask questions rather than assume intent.
* If you're going to nitpick, preface the comment with "nit:". This means the author can choose to ignore the comment.

### For Authors

As the author of a pull request, your responsibility is to ensure the review process is smooth, transparent, and productive.

#### Before Requesting a Review

* Review your own code. Catch obvious issues and clean up unnecessary changes.
* Some code changes are too large to be reviewed quickly. This can happen when the number of lines of new code is more than a few hundred. Consider breaking up your code in this case.
* Write a clear PR description. Include context, reasoning, and anything reviewers should know up front.
* Add tests and verify they pass locally and in CI.

#### During Review

* Respond to each comment, even if just to acknowledge it.
* Use GitHub's "Resolve" feature when you've addressed feedback. In some cases, to get to the "Resolve" button requires you select "Hide" first, with a reason of "Resolved".
* Don't be afraid to explain your design decisions—but stay open to change.
* If you disagree with a reviewer's suggestion, provide reasoning. If you're sure your response fully resolves the reviewer's suggestion, mark it as resolved.

#### After Review

* When you've made requested changes, clearly indicate it in your comment or commit, and re-request the review.
* If the PR was rejected, wait for explicit re-approval before merging.
* Thank your reviewers—they're helping you ship better code.

## [How to report a bug]

Please use the [issue tracker](https://github.com/ava-labs/firewood/issues) for reporting issues.

## [First time fixes for contributors]

The [issue tracker](https://github.com/ava-labs/firewood/issues) typically has some issues tagged for first-time contributors. If not,
please reach out. We hope you work on an easy task before tackling a harder one.

## [How to request an enhancement]

Just like bugs, please use the [issue tracker](https://github.com/ava-labs/firewood/issues) for requesting enhancements. Please tag the issue with the "enhancement" tag.

## [Style Guide / Coding Conventions]

We generally follow the same rules that `cargo fmt` and `cargo clippy` will report as warnings, with a few notable exceptions as documented in the associated Cargo.toml file.

By default, we prohibit bare `unwrap` calls and index dereferencing, as there are usually better ways to write this code. In the case where you can't, please use `expect` with a message explaining why it would be a bug, which we currently allow. For more information on our motivation, please read this great article on unwrap: [Using unwrap() in Rust is Okay](https://blog.burntsushi.net/unwrap) by [Andrew Gallant](https://blog.burntsushi.net).

## [Where can I ask for help]?

If you have questions or need help, please post them as issues in the [issue tracker](https://github.com/ava-labs/firewood/issues). This allows the community to benefit from the discussion and helps us maintain a searchable knowledge base.

## Thank you

We'd like to extend a pre-emptive "thank you" for reading through this and submitting your first contribution!
