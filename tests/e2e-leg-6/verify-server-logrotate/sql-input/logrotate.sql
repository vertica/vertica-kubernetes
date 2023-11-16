-- Cleanup the DC table
select clear_data_collector('LogRotateOperations');

-- Logrotate service turn on by default, run with default configurations
select hurry_service('SYSTEM', 'LogRotate', 60);
    
-- Check the system view, should show 2 records
SELECT * FROM log_rotate_events ORDER BY node_name;

-- Set LogRotateMaxSize to 1k, should rotate log next time
ALTER DATABASE default set LogRotateMaxSize = '1k';

-- Run with new LogRotateMaxSize configuration
select hurry_service('SYSTEM', 'LogRotate', 60);
\! sleep 1
    
-- Check the system view on node01, and 2 records is added after run with new LogRotateMaxSize configuration 
SELECT node_name, success, max_size, max_age, log_file, need_rotation FROM log_rotate_events ORDER BY node_name;

-- Set LogRotateMaxAge to 1sec
ALTER DATABASE default set LogRotateMaxAge = '1s';
select hurry_service('SYSTEM', 'LogRotate', 60);

-- Check the system view on node01
SELECT node_name, success, max_size, max_age, log_file, need_rotation FROM log_rotate_events ORDER BY node_name;

-- Cleanup the DC table and turn off the timer service, should not run
select clear_data_collector('LogRotateOperations');
ALTER DATABASE default set EnableLogRotate = 0;
select hurry_service('SYSTEM', 'LogRotate', 60);
SELECT * FROM log_rotate_events ORDER BY node_name;

-- Cleanup configurations
ALTER DATABASE default clear LogRotateMaxSize;
ALTER DATABASE default clear LogRotateMaxAge;