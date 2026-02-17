#!/usr/bin/python3
import argparse
import base64
import json
import os
import random
import requests
import teslapy
import time
import sys
from datetime import datetime, timedelta
# Only used for debugging.
from pprint import pprint


# Global vars for use by various functions.
list_url = 'https://owner-api.teslamotors.com/api/1/products'
base_url = 'https://owner-api.teslamotors.com/api/1/vehicles'
SETTINGS = {
    'DEBUG': False,
    'refresh_token': False,
    'tesla_email': 'dummy@local',
    'tesla_password': '',
    'tesla_access_token': '',
    'tesla_vin': '',
}
date_format = '%Y-%m-%d %H:%M:%S'
# This dict stores the data that will be written to /mutable/tesla_api.json.
# we load its contents from disk at the start of the script, and save them back
# to the disk whenever the contents change.
tesla_api_json = {
    'access_token': '',
    'refresh_token': '',
    'id': 0,
    'vehicle_id': 0,
}

mutable_dir = '/mutable'

def _invalidate_access_token():
    if not tesla_api_json.get('refresh_token') or tesla_api_json['refresh_token'] == '':
        tesla_api_json['refresh_token'] = SETTINGS['refresh_token']
    tesla_api_json['access_token'] = None
    _write_tesla_api_json()

def _execute_request(url=None, method=None, data=None, require_vehicle_online=True):
    """
    Wrapper around requests to the Tesla REST Service which ensures the vehicle is online before proceeding
    :param url: the url to send the request to
    :param method: the request method ('GET' or 'POST')
    :param data: the request data (optional)
    :return: JSON response
    """
    if require_vehicle_online:
        vehicle_online = False
        while not vehicle_online:
            _log("Attempting to wake up Vehicle (ID:{})".format(tesla_api_json['id']))
            result = _rest_request(
                '{}/{}/wake_up'.format(base_url, tesla_api_json['id']),
                method='POST'
            )

            # Tesla REST Service sometimes misbehaves... this seems to be caused by an invalid/expired auth token
            # TODO: Remove auth token and retry?
            if result.get('response') is None:
                _error(f"Fatal Error: Tesla REST Service returned an invalid response: {result}")
                sys.exit(1)

            vehicle_online = result['response']['state'] == "online"
            if vehicle_online:
                _log("Vehicle (ID:{}) is Online".format(tesla_api_json['id']))
            else:
                _log("Vehicle (ID:{}) is Asleep; Waiting 5 seconds before retry...".format(tesla_api_json['id']))
                time.sleep(5)

    if url is None:
        return result['response']['state']

    json_response = _rest_request(url, method, data)

    # Error handling
    error = json_response.get('error')
    if error:
        # Log error and die
        _error(json.dumps(json_response, indent=2))
        sys.exit(1)

    return json_response


def _rest_request(url, method=None, data=None):
    """
    Executes a REST request
    :param url: the url to send the request to
    :param method: the request method ('GET' or 'POST')
    :param data: the request data (optional)
    :return: JSON response
    """
    # set default method value
    if method is None:
        method = 'GET'
    # set default data value
    if data is None:
        data = {}
    headers = {
      'Authorization': 'Bearer {}'.format(_get_api_token()),
      'User-Agent': 'github.com/Scottmg1/Sentry-USB',
    }

    _log("Sending {} Request: {}; Data: {}".format(method, url, data))
    if method.upper() == 'GET':
        response = requests.get(url, headers=headers)
    elif method.upper() == 'POST':
        response = requests.post(url, headers=headers, data=data)
    else:
        raise ValueError('Unsupported Request Method: {}'.format(method))
    if not response.text:
        _error("Fatal Error: Tesla REST Service failed to return a response, access token may have expired")
        sys.exit(1)
    if  'invalid bearer token' in response.text:
        _error("Invalid Access token, removing from cache...")
        _invalidate_access_token()
    json_response = response.json()

    # log full JSON response for debugging
    _log(json.dumps(json_response, indent=2))

    return json_response


def _get_api_token():
    """
    Retrieves the API access token, either from /mutable/tesla_api.json,
    SETTINGS, or from the Tesla API by using the credentials in SETTINGS.
    If those are also not available, kill the script, since it can't continue.
    """
    os.chdir(mutable_dir)
    # If the token was already saved, work with that.
    if tesla_api_json['access_token']:
        # Due to what appears to be a bug with the fake-hwclock service,
        # sometimes the system thinks it's still November 2016. If that's the
        # case, we can't accurately determine the age of the token, so we just
        # use it. Later executions of the script should run after the date has
        # updated correctly, at which point we can properly compare the dates.
        now = datetime.now()
        if now.year < 2019: # This script was written in 2019.
            return tesla_api_json['access_token']

        if not SETTINGS.get('refresh_token'):
            _log('No refreshing token available. Using existing access token.')
            return tesla_api_json['access_token']

        tesla = teslapy.Tesla(SETTINGS['tesla_email'], None)
        # For some reason, the `expires_at` timestamp doesn't exactly match the `exp` field in the JWT body.
        # The `expires_at` is typically about 1 minute ahead of the `exp` value in the JWT payload.
        # Access token usually expires after a few hours, so refresh the token if it has less than `expiration_buffer_s` seconds remaining.
        expiration_buffer_s = 60 * 30
        if tesla.expires_at <= time.time() - expiration_buffer_s:
            _log("Refreshing expired access token...")
            tesla.token['refresh_token'] = SETTINGS.get('refresh_token')
            tesla.refresh_token()

        if tesla_api_json['access_token'] != tesla.token.get('access_token'):
            _log("Syncing access token...")
            tesla_api_json['access_token'] = tesla.token.get('access_token')
            _write_tesla_api_json()
        return tesla_api_json['access_token']

    # If the access token is not already stored in tesla_api_json AND
    # the user provided a refresh_token force it into the client to get a proper token
    elif tesla_api_json['refresh_token']:
        tesla = teslapy.Tesla(SETTINGS['tesla_email'], None)
        _log('Force setting a refresh token')
        tesla.access_token = "DUMMY"
        tesla.token['refresh_token'] = tesla_api_json['refresh_token']
        tesla.refresh_token()
        tesla_api_json['access_token'] = tesla.token.get('access_token')
        # if the refresh token is changed we store the new one, never saw it happen but...
        tesla_api_json['refresh_token'] = tesla.token['refresh_token']
        _write_tesla_api_json()
        return tesla_api_json['access_token']

    _error('Unable to perform Tesla API functions: no credentials or token.')
    sys.exit(1)


def _get_id():
    """
    Put the vehicle's ID into tesla_api_json['id'].
    """
    # If it was already set by _load_tesla_api_json(), and a new
    # VIN or name wasn't specified on the command line, we're done.
    if tesla_api_json['id'] and tesla_api_json['vehicle_id']:
      if SETTINGS['tesla_name'] == '' and SETTINGS['tesla_vin'] == '':
        return

    # Call list_vehicles() and use the provided name or VIN to get the vehicle ID.
    result = list_vehicles()
    for vehicle_dict in result['response']:
        if ( ( SETTINGS['tesla_vin'] != '' and vehicle_dict['vin'] == SETTINGS['tesla_vin'] )
          or ( SETTINGS['tesla_name'] != '' and vehicle_dict['display_name'] == SETTINGS['tesla_name'] )
          or ( SETTINGS['tesla_vin'] == '' and SETTINGS['tesla_name'] == '')):
            tesla_api_json['id'] = vehicle_dict['id_s']
            tesla_api_json['vehicle_id'] = vehicle_dict['vehicle_id']
            _log('Retrieved Vehicle ID from Tesla API.')
            _write_tesla_api_json()
            return

    _error('Unable to retrieve vehicle ID: Unknown name or VIN. Cannot continue.')
    sys.exit(1)


def _load_tesla_api_json():
    """
    Load the data stored in /mutable/tesla_api.json, if it exists.
    If it doesn't exist, write a file to that location with default values.
    """
    try:
        with open(mutable_dir + '/tesla_api.json', 'r') as f:
            _log('Loading mutable data from disk...')
            json_string = f.read()
    except FileNotFoundError:
        # Write a dict with the default data to the file.
        _log("Mutable data didn't exist, writing defaults...")
        _write_tesla_api_json()
    else:
        def datetime_parser(dct):
            # Converts any string with the appropriate format in the parsed JSON
            # dict into a datetime object.
            for k, v in dct.items():
                try:
                    dct[k] = datetime.strptime(v, date_format)
                except (TypeError, ValueError):
                    pass
            return dct

        # Need to declare this as a global since we assign to it directly.
        global tesla_api_json
        tesla_api_json = json.loads(json_string, object_hook=datetime_parser)


def _write_tesla_api_json():
    """
    Write the contents of the tesla_api_json dict to /mutable/tesla_api.json.
    """
    def convert_dt(obj):
        # Converts datetime objects into 'YYYY-MM-DD HH:MM:SS' strings, since
        # json.dumps() can't serialize them itself.
        if isinstance(obj, datetime):
            return obj.strftime(date_format)

    with open(mutable_dir + '/tesla_api.json', 'w') as f:
        _log('Writing ' + mutable_dir + '/tesla_api.json...')
        json_string = json.dumps(tesla_api_json, indent=2, default=convert_dt)
        f.write(json_string)


def _get_log_timestamp():
    # I can't figure out how to get a timezone aware version of now() in
    # Python 2.7 without pytz, so I kludged this together. It outputs the
    # same timestamp format as the other logging done by SentryUSB's code.
    zone = time.tzname[time.daylight]
    return datetime.now().strftime('%a %d %b %H:%M:%S {} %Y'.format(zone))


def _log(msg, flush=True):
    if SETTINGS['DEBUG']:
        print("{}: {}".format(_get_log_timestamp(), msg), flush=flush)


def _error(msg, flush=True):
    """
    It's _log(), but for errors, so it always prints.
    """
    print("{}: {}".format(_get_log_timestamp(), msg), file=sys.stderr, flush=flush)


######################################
# API GET Functions
######################################
def list_vehicles():
    return _execute_request(list_url, None, None, False)


def get_service_data():
    return _execute_request(
        '{}/{}/service_data'.format(base_url, tesla_api_json['id'])
    )


def get_vehicle_summary():
    return _execute_request(
        '{}/{}'.format(base_url, tesla_api_json['id'])
    )


def get_vehicle_legacy_data():
    return _execute_request(
        '{}/{}/data'.format(base_url, tesla_api_json['id'])
    )


def get_nearby_charging():
    return _execute_request(
        '{}/{}//nearby_charging_sites'.format(base_url, tesla_api_json['id'])
    )


def get_vehicle_data():
    return _execute_request(
        '{}/{}/vehicle_data'.format(base_url, tesla_api_json['id'])
    )


def get_vehicle_online_state():
    # list_vehicles gets the state of each vehicle without waking them up
    result = list_vehicles()
    for vehicle_dict in result['response']:
        if ( vehicle_dict['vehicle_id'] == tesla_api_json['vehicle_id']):
            return vehicle_dict['state']
    _error("Could not find vehicle");
    sys.exit(1)

def is_vehicle_online():
    return get_vehicle_online_state() == "online"


def get_charge_state():
    return _execute_request(
        '{}/{}/data_request/charge_state'.format(base_url, tesla_api_json['id'])
    )


def get_climate_state():
    return _execute_request(
        '{}/{}/data_request/climate_state'.format(base_url, tesla_api_json['id'])
    )


def get_drive_state():
    return _execute_request(
        '{}/{}/data_request/drive_state'.format(base_url, tesla_api_json['id'])
    )


def get_gui_settings():
    return _execute_request(
        '{}/{}/data_request/gui_settings'.format(base_url, tesla_api_json['id'])
    )


def get_vehicle_state():
    return _execute_request(
        '{}/{}/data_request/vehicle_state'.format(base_url, tesla_api_json['id'])
    )


######################################
# Custom Functions
######################################
def get_odometer():
    data = get_vehicle_state()
    return int(data['response']['odometer'])


def is_car_locked():
    data = get_vehicle_state()
    return data['response']['locked']


def is_sentry_mode_enabled():
    data = get_vehicle_state()
    return data['response']['sentry_mode']


'''
This accesses the streaming endpoint, but doesn't
stick around to wait for continuous results.
'''
def streaming_ping():
    # the car needs to be awake for the streaming endpoint to work
    wake_up_vehicle()

    headers = {
      'User-Agent': 'github.com/Scottmg1/Sentry-USB',
      'Authorization': 'Bearer {}'.format(_get_api_token()),
      'Connection': 'Upgrade',
      'Upgrade': 'websocket',
      'Sec-WebSocket-Key': base64.b64encode(bytes([random.randrange(0, 256) for _ in range(0, 16)])).decode('utf-8'),
      'Sec-WebSocket-Version': '13',
    }

    url = 'https://streaming.vn.teslamotors.com/connect/{}'.format(tesla_api_json['vehicle_id'])

    _log("Sending streaming request")
    response = requests.get(url, headers=headers, stream=True)
    if not response:
        _error("Fatal Error: Tesla REST Service failed to return a response, access token may have expired")
        sys.exit(1)

    return response


######################################
# API POST Functions
######################################
def wake_up_vehicle():
    _log('Sending wakeup API command...')
    return _execute_request()

def set_charge_limit(percent):
    return _execute_request(
        '{}/{}/command/set_charge_limit'.format(base_url, tesla_api_json['id']),
        method='POST',
        data={'percent': percent}
    )

def actuate_trunk():
    result = _execute_request(
        '{}/{}/command/actuate_trunk'.format(base_url, tesla_api_json['id']),
        method='POST',
        data={'which_trunk': 'rear'}
    )
    return result['response']['result']

def actuate_frunk():
    result = _execute_request(
        '{}/{}/command/actuate_trunk'.format(base_url, tesla_api_json['id']),
        method='POST',
        data={'which_trunk': 'front'}
    )
    return result['response']['result']

def flash_lights():
    result = _execute_request(
        '{}/{}/command/flash_lights'.format(base_url, tesla_api_json['id']),
        method='POST'
    )
    return result['response']['result']

def set_sentry_mode(enabled: bool):
    """
    Activates or deactivates Sentry Mode based on the 'enabled' parameter
    :param enabled: True to Enable Sentry Mode; False to Disable Sentry Mode
    :return: True if the command was successful
    """
    _log("Setting Sentry Mode Enabled: {}".format(enabled))
    result = _execute_request(
        '{}/{}/command/set_sentry_mode'.format(base_url, tesla_api_json['id']),
        method='POST',
        data={'on': enabled}
    )
    return result['response']['result']


def enable_sentry_mode():
    """
    Enables Sentry Mode
    :return: Human-friendly String indicating command success/failure
    """
    if True == set_sentry_mode(True):
        return "Success: Sentry Mode Enabled"
    else:
        return "Failed to Enable Sentry Mode"


def disable_sentry_mode():
    """
    Disables Sentry Mode
    :return: Human-friendly String indicating command success/failure
    """
    if True == set_sentry_mode(False):
        return "Success: Sentry Mode Disabled"
    else:
        return "Failed to Disable Sentry Mode"


def toggle_sentry_mode():
    """
    Activates Sentry Mode if it is currently off, disables it if it is currently on
    :return: True if the command was successful
    """
    if is_sentry_mode_enabled():
        return disable_sentry_mode()
    else:
        return enable_sentry_mode()


######################################
# Utility Functions
######################################
def _get_api_functions():
    # Build the list of available Tesla API function names by getting the
    # callables from globals() and skipping the non-API functions.
    non_api_names = ['main', 'pprint', 'datetime', 'timedelta']
    function_names = []
    for name, func in globals().items():
        if (callable(func)
                and not name.startswith('_')
                and name not in non_api_names):
            function_names.append(name)
    function_names.sort()
    function_names_string = '\n'.join(function_names)

    return function_names_string


def _get_arg_parser():
    # Parse the CLI arguments.
    parser = argparse.ArgumentParser()
    parser.add_argument(
        'function',
        help="The name of the function to run. Available functions are:\n {}".format(_get_api_functions()))
    parser.add_argument(
        '--arguments',
        help="Add arguments to the function by passing comma-separated key:value pairs."
    )
    parser.add_argument(
        "--debug",
        action="store_true",
        help="Print debug output."
    )
    parser.add_argument(
        "--refresh_token",
        help="Tesla refresh_token to authenticate."
    )
    parser.add_argument(
        "--vin",
        help="VIN number of the car."
    )
    parser.add_argument(
        "--name",
        help="name of the car."
    )

    return parser


######################################
# MAIN
######################################
def main():
    args = _get_arg_parser().parse_args()

    SETTINGS['DEBUG'] = args.debug
    SETTINGS['refresh_token'] = args.refresh_token

    if args.vin:
        SETTINGS['tesla_vin'] = args.vin
    else:
        SETTINGS['tesla_vin'] = os.environ.get('TESLA_VIN', '')

    if args.refresh_token:
        SETTINGS['refresh_token'] = args.refresh_token
    else:
        SETTINGS['refresh_token'] = os.environ.get('TESLA_REFRESH_TOKEN', '')

    if args.name:
        SETTINGS['tesla_name'] = args.name
    else:
        SETTINGS['tesla_name'] = os.environ.get('TESLA_NAME', '')

    # We call this now so DEBUG will be set correctly.
    _load_tesla_api_json()

    if not tesla_api_json.get('refresh_token') or tesla_api_json['refresh_token'] == '':
        tesla_api_json['refresh_token'] = SETTINGS['refresh_token']
        _write_tesla_api_json()

    # Apply any arguments that the user may have provided.
    kwargs = {}
    if args.arguments:
        for kwarg_string in [arg.strip() for arg in args.arguments.split(',')]:
            key, value = kwarg_string.split(':')
            kwargs[key] = value
    # Render the arguments as a POST body.
    kwargs_string = ''
    if kwargs:
        kwargs_string = ', '.join(
            '{}={}'.format(key, value) for key, value in kwargs.items()
        )

    # We need to call this before calling any API function, because those need
    # to know the ID before they call _execute_request()
    _get_id()

    # Get the function by name from the globals() dict and call it with the
    # specified args.
    function = globals()[args.function]
    _log('Calling {}({})...'.format(args.function, kwargs_string))
    result = function(**kwargs)

    # Write the output of the API call to stdout, if DEBUG is true.
    is_json = False
    try:
        # check to see if result is json
        if isinstance(result, str):
            json.loads(result)
            is_json = True
    except ValueError as e:
        pass

    if is_json:
        _log(json.dumps(result, indent=2))
    else:
        print(result, flush=True)


main()
