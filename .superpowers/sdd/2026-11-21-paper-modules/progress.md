# SDD ledger — plan: docs/superpowers/plans/2026-11-21-paper-modules.md

Pre-flight: Task 2 consumes ledger.Book from Task 1. Task 3 consumes Plan/Intent from Task 2 and Apply from Task 1. Task 5 consumes store + matching. Task 6 consumes jobs.OpenAccount. No signature conflicts.

Ruling: implement on feat/paper-modules in the current workspace — user said start executing; unrelated dirty files already on master stay on this branch.
