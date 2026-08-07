# rmgedcomx tests

For testing the write functionality of the rmgedcomx server a suite of tests have been run manually
in RootsMagic, as shown in the table below. Any changes to the database have been captured (using
the [sqldiff](https://sqlite.org/sqldiff.html) tool) as a "Golden file" in the `testdata` directories.

The Go tests will perform the same writes via the rmgedcomx API, use sqldiff to compare the modified
database to the original database, and then compare the sqldiff output to the "Golden files" to find
any issues.

Dynamic data, such as timestamps and GUIDs, cannot be expected to match between these tests so these
fields are identified and replaced with placeholders in the sqldiff output. The comparison with the
golden file will then show whether the dynamic data was changed or not without getting hung up on the
value.

| Test ID | Test Description | Test output |