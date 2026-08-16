import sys
import re
import requests

def parse_gedcom(filename):
    """Parses a GEDCOM file into a dictionary of individuals and families."""
    records = {'INDI': {}, 'FAM': {}}
    current_id, current_type, current_tag = None, None, None

    with open(filename, 'r', encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if not line: continue

            parts = line.split(' ', 2)
            level = int(parts[0])
            tag = parts[1]
            value = parts[2] if len(parts) > 2 else ""

            if level == 0:
                if value in ['INDI', 'FAM']:
                    current_id = tag
                    current_type = value
                    records[current_type][current_id] = {'id': current_id, 'events': {}}
                else:
                    current_id = None
            elif level == 1 and current_id:
                if tag == 'SEX':
                    records[current_type][current_id][tag] = value
                elif tag == 'NAME':
                    current_tag = tag
                    records[current_type][current_id][tag] = value
                    records[current_type][current_id]['name_parts'] = {}
                elif tag in ['HUSB', 'WIFE', 'CHIL']:
                    records[current_type][current_id].setdefault(tag, []).append(value)
                elif tag in ['BIRT', 'CHR', 'DEAT', 'BURI', 'MARR']:
                    current_tag = tag
                    records[current_type][current_id]['events'][current_tag] = {}
            elif level == 2 and current_id and current_tag:
                # Capture Level 2 name parts (GIVN, SURN)
                if current_tag == 'NAME' and tag in ['GIVN', 'SURN']:
                    records[current_type][current_id]['name_parts'][tag] = value
                # Capture Level 2 event details (DATE, PLAC)
                elif current_tag in ['BIRT', 'CHR', 'DEAT', 'BURI', 'MARR'] and tag in ['DATE', 'PLAC']:
                    records[current_type][current_id]['events'][current_tag][tag] = value

    return records

def normalize_gedcom_date(date_str):
    """Converts a typical GEDCOM date string into a GEDCOM X formal date string."""
    if not date_str:
        return None

    months = {
        'JAN': '01', 'FEB': '02', 'MAR': '03', 'APR': '04',
        'MAY': '05', 'JUN': '06', 'JUL': '07', 'AUG': '08',
        'SEP': '09', 'OCT': '10', 'NOV': '11', 'DEC': '12'
    }

    parts = date_str.strip().upper().split()
    if not parts:
        return None

    prefix = ""
    suffix = ""
    is_approximate = False

    # Handle GEDCOM approximation and bounds modifiers
    if parts[0] in ['ABT', 'CAL', 'EST']:
        is_approximate = True
        parts = parts[1:]
    elif parts[0] == 'BEF':
        prefix = "/"
        parts = parts[1:]
    elif parts[0] == 'AFT':
        suffix = "/"
        parts = parts[1:]

    year, month, day = None, None, None
    for part in reversed(parts):
        if part.isdigit():
            if len(part) in (3, 4):
                year = part
            elif len(part) in (1, 2):
                day = part.zfill(2)
        elif part in months:
            month = months[part]

    if not year:
        return None

    # Construct standard simple date format +YYYY[-MM[-DD]]
    formal_date = f"+{year}"
    if month:
        formal_date += f"-{month}"
        if day:
            formal_date += f"-{day}"

    # Apply GEDCOM X formal spec modifiers
    if is_approximate:
        formal_date = f"A{formal_date}"

    return f"{prefix}{formal_date}{suffix}"

def build_person_doc(ind_id, ind_data):
    """Builds a GEDCOM X document for a single person, including fallback NAME solidus parsing and formal dates."""
    person = {"names": [], "facts": []}

    if 'NAME' in ind_data:
        raw_name = ind_data['NAME']
        name_str = raw_name.replace('/', '').strip()
        name_form = {"fullText": name_str}

        parts = []

        # 1. Prefer explicitly parsed Level 2 GIVN and SURN elements
        if 'name_parts' in ind_data and ind_data['name_parts']:
            if 'GIVN' in ind_data['name_parts']:
                parts.append({"type": "http://gedcomx.org/Given", "value": ind_data['name_parts']['GIVN']})
            if 'SURN' in ind_data['name_parts']:
                parts.append({"type": "http://gedcomx.org/Surname", "value": ind_data['name_parts']['SURN']})

        # 2. Fallback: Parse the solidus characters in the raw NAME string
        elif '/' in raw_name:
            match = re.search(r'^(.*?)/(.*?)/(.*?)$', raw_name)
            if match:
                given1, surname, given2 = match.groups()
                given = (given1.strip() + " " + given2.strip()).strip()
                surname = surname.strip()

                if given:
                    parts.append({"type": "http://gedcomx.org/Given", "value": given})
                if surname:
                    parts.append({"type": "http://gedcomx.org/Surname", "value": surname})

        if parts:
            name_form["parts"] = parts

        person['names'].append({"nameForms": [name_form]})

    if 'SEX' in ind_data:
        gender_type = "http://gedcomx.org/Male" if ind_data['SEX'] == 'M' else "http://gedcomx.org/Female"
        person['gender'] = {"type": gender_type}

    event_mapping = {
        'BIRT': 'http://gedcomx.org/Birth', 'DEAT': 'http://gedcomx.org/Death',
        'CHR': 'http://gedcomx.org/Christening', 'BURI': 'http://gedcomx.org/Burial'
    }

    for event_tag, event_data in ind_data.get('events', {}).items():
        if event_tag in event_mapping:
            fact = {"type": event_mapping[event_tag]}
            if 'DATE' in event_data:
                fact["date"] = {"original": event_data['DATE']}
                formal_date = normalize_gedcom_date(event_data['DATE'])
                if formal_date:
                    fact["date"]["formal"] = formal_date
            if 'PLAC' in event_data:
                fact["place"] = {"original": event_data['PLAC']}
            person['facts'].append(fact)

    return {"persons": [person]}

def build_relationship_doc(rel_type, person1_uri, person2_uri, facts=None):
    """Builds a GEDCOM X document for a single relationship."""
    rel = {
        "type": rel_type,
        "person1": {"resource": person1_uri},
        "person2": {"resource": person2_uri}
    }
    if facts:
        rel["facts"] = facts
    return {"relationships": [rel]}

def discover_endpoints(server_url, headers):
    """GETs the root collection to discover hypermedia links."""
    r = requests.get(server_url, headers=headers)
    r.raise_for_status()

    data = r.json()
    collections = data.get('collections', [])
    if not collections:
        raise ValueError("No collections found at the root URL. Cannot proceed.")

    first_collection = collections[0]
    links = first_collection.get('links', {})

    persons_url = links.get('persons', {}).get('href')
    rels_url = links.get('relationships', {}).get('href')

    if not persons_url or not rels_url:
        raise ValueError("Could not discover 'persons' or 'relationships' endpoints.")

    return persons_url, rels_url

def upload_data(server_url, gedcom_data):
    """Executes the state machine: discover -> create persons -> create relationships."""
    headers = {
        "Content-Type": "application/x-gedcomx-v1+json",
        "Accept": "application/x-gedcomx-v1+json"
    }

    try:
        print(f"Discovering endpoints at {server_url} ...")
        persons_url, rels_url = discover_endpoints(server_url, headers)

        id_map = {}

        print(f"Uploading individuals to {persons_url} ...")
        for ind_id, ind_data in gedcom_data['INDI'].items():
            doc = build_person_doc(ind_id, ind_data)
            r = requests.post(persons_url, json=doc, headers=headers)
            r.raise_for_status()

            server_uri = r.headers.get('Location')
            if not server_uri:
                raise ValueError(f"Server did not return a Location header for person {ind_id}")
            id_map[ind_id] = server_uri

        print(f"Uploading relationships to {rels_url} ...")
        for fam_id, fam_data in gedcom_data['FAM'].items():
            husbands = fam_data.get('HUSB', [])
            wives = fam_data.get('WIFE', [])
            children = fam_data.get('CHIL', [])

            for husb in husbands:
                for wife in wives:
                    if husb in id_map and wife in id_map:
                        facts = []
                        if 'MARR' in fam_data.get('events', {}):
                            marr = fam_data['events']['MARR']
                            fact = {"type": "http://gedcomx.org/Marriage"}
                            if 'DATE' in marr:
                                fact["date"] = {"original": marr['DATE']}
                                formal_date = normalize_gedcom_date(marr['DATE'])
                                if formal_date:
                                    fact["date"]["formal"] = formal_date
                            if 'PLAC' in marr:
                                fact["place"] = {"original": marr['PLAC']}
                            facts.append(fact)

                        doc = build_relationship_doc("http://gedcomx.org/Couple", id_map[husb], id_map[wife], facts)
                        r = requests.post(rels_url, json=doc, headers=headers)
                        r.raise_for_status()

            parents = husbands + wives
            for parent in parents:
                for child in children:
                    if parent in id_map and child in id_map:
                        doc = build_relationship_doc("http://gedcomx.org/ParentChild", id_map[parent], id_map[child])
                        r = requests.post(rels_url, json=doc, headers=headers)
                        r.raise_for_status()

        print("Upload completed successfully!")

    except requests.exceptions.RequestException as e:
        print(f"HTTP Error: {e}")
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: python gedcom_to_gedcomx.py <gedcom_filename> <gedcomx_server_url>")
        sys.exit(1)

    filename = sys.argv[1]
    server_url = sys.argv[2]

    print(f"Parsing {filename}...")
    parsed_data = parse_gedcom(filename)
    upload_data(server_url, parsed_data)
