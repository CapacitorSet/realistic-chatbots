realistic-chatbots
==================

A toolkit for creating realistic chatbots from chat logs.

## Usage (draft)

 * Use char-rnn (or one of its forks) to generate `delays.txt`, which contains a list of delays (integer, base 10) separated by \n.
 * Create training data. It must be a list of JSON objects with the format {time, text} (other keys are ignored), separated by \n.
 * Install dependencies with `dep ensure -update -v`.
 * Compile the application, run it, and type `help` to get started.