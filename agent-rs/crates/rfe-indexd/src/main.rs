use std::io::{stdin, stdout, BufReader, BufWriter};

fn main() {
    // stdout carries frames only; anything diagnostic goes to stderr, which the Go agent logs.
    let input = BufReader::new(stdin().lock());
    let output = BufWriter::new(stdout());
    if let Err(e) = rfe_indexd::server::serve(input, output) {
        eprintln!("rfe-indexd: {e}");
        std::process::exit(1);
    }
}
